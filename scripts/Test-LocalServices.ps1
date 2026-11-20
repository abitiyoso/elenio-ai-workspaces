param([Parameter(Mandatory)][string]$PostgresBin,[Parameter(Mandatory)][string]$GoExe,[string[]]$Modules=@('flowcase','control-plane','agent','kms','ucw'))
$ErrorActionPreference='Stop'
$repo=Split-Path $PSScriptRoot -Parent
$state=Join-Path $repo '.local-e2e'
$go=(Resolve-Path $GoExe).Path;$pg=(Resolve-Path $PostgresBin).Path
$secret=[IO.File]::ReadAllText((Join-Path $state 'database-password'))
$env:PGPASSWORD=$secret;$env:ELENIO_LOCAL_E2E='1'
$records=@()
$targets=@{
 'flowcase'=@{dir='flowcase_platform';db='flowcase_tests';variable='FLOWCASE_TEST_DATABASE_URL'}
 'control-plane'=@{dir='control_plane';db='control_plane_tests';variable='CP_TEST_DATABASE_URL'}
 'agent'=@{dir='ai_agent_studio';db='agent_tests';variable='AGENT_STUDIO_TEST_DATABASE_URL'}
 'kms'=@{dir='kms_go_package/backend';db='kms_tests';variable='KMS_TEST_DATABASE_URL'}
 'ucw'=@{dir='universal_case_workspace';db='ucw_tests';variable='UCW_TEST_DATABASE_URL'}
}
foreach($name in $Modules){
 if(!$targets.ContainsKey($name)){throw 'Unknown local test module'}
 $target=$targets[$name]
 [Environment]::SetEnvironmentVariable($target.variable,"postgres://elenio_test:$secret@127.0.0.1:55448/$($target.db)?sslmode=disable",'Process')
 $env:FLOWCASE_REQUIRE_DB='1';$env:CP_REQUIRE_DB='1';$env:AGENT_STUDIO_TEST_DATABASE_DISPOSABLE='1'
 Push-Location (Join-Path $repo $target.dir)
 try {
  if($name -eq 'agent'){
   $exists=& "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d agent_tests -Atc "SELECT to_regclass('public.agents') IS NOT NULL"
   if($LASTEXITCODE -ne 0){throw 'Local database check failed'}
   if($exists -eq 'f'){foreach($sql in Get-ChildItem migrations -Filter '*.up.sql'|Sort-Object Name){& "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d agent_tests -v ON_ERROR_STOP=1 -f $sql.FullName > (Join-Path $state 'agent-migrate.log');if($LASTEXITCODE -ne 0){throw 'Agent migration failed'}}}
   # shortcut: upgrade the pre-existing disposable fixture's new usage table; centralize fixture migrations when another additive schema is introduced.
   $usageExists=& "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d agent_tests -Atc "SELECT to_regclass('public.agent_run_usage') IS NOT NULL"
   if($LASTEXITCODE -ne 0){throw 'Local usage schema check failed'}
   if($usageExists -eq 'f'){& "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d agent_tests -v ON_ERROR_STOP=1 -1 -f migrations/000006_run_usage.up.sql > (Join-Path $state 'agent-migrate.log');if($LASTEXITCODE -ne 0){throw 'Agent usage migration failed'}}
  }
  if($name -eq 'kms'){& $go run ./tools/local-test-migrate; if($LASTEXITCODE -ne 0){throw 'KMS migration failed'}}
  $log=Join-Path $state "$name-tests.jsonl"
  & $go test -json -count=1 -p 1 ./... > $log
  $code=$LASTEXITCODE
  $events=@(Get-Content $log|ForEach-Object{try{$_|ConvertFrom-Json}catch{}}|Where-Object {$_.Test -and !$_.Test.Contains('/') -and $_.Action -in 'pass','fail','skip'})
  $records+= [ordered]@{module=$name;exit_code=$code;passed=@($events|Where-Object Action -eq 'pass').Count;failed=@($events|Where-Object Action -eq 'fail').Count;skipped=@($events|Where-Object Action -eq 'skip').Count;failures=@($events|Where-Object Action -eq 'fail'|ForEach-Object Test)}
  Write-Output "$name finished, exit $code"
 }finally{Pop-Location;[Environment]::SetEnvironmentVariable($target.variable,$null,'Process')}
}
Remove-Item Env:PGPASSWORD -ErrorAction SilentlyContinue
$summary=[ordered]@{date=(Get-Date -Format 'yyyy-MM-dd');database='PostgreSQL 16.16, loopback-only disposable databases';test_kind='real database integration; provider/IdP doubles remain where existing tests specify them';modules=$records}
$summary|ConvertTo-Json -Depth 6|Set-Content (Join-Path $state 'services-summary.json')
$summary|ConvertTo-Json -Depth 6
if(@($records|Where-Object {$_.exit_code -ne 0}).Count){exit 1}
