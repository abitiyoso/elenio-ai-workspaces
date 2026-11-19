param(
  [Parameter(Mandatory)][string]$PostgresBin,
  [Parameter(Mandatory)][string]$GoExe,
  [switch]$DatabaseOnly
)
$ErrorActionPreference='Stop'
$repo=Split-Path $PSScriptRoot -Parent
$state=Join-Path $repo '.local-e2e'
[IO.Directory]::CreateDirectory($state)|Out-Null
$pg= (Resolve-Path $PostgresBin).Path
$go=(Resolve-Path $GoExe).Path
$data=Join-Path $state 'postgres-data'
$pw=Join-Path $state 'database-password'
if(!(Test-Path $pw)){[IO.File]::WriteAllText($pw,[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(24)))}
$env:PGPASSWORD=[IO.File]::ReadAllText($pw)
function Check {if($LASTEXITCODE -ne 0){throw "Local command failed ($LASTEXITCODE)."}}
if(!(Test-Path (Join-Path $data 'PG_VERSION'))){
  & "$pg/initdb.exe" -D $data -U elenio_test --pwfile=$pw --auth-host=scram-sha-256 --auth-local=scram-sha-256 --encoding=UTF8 --locale=C; Check
}
& "$pg/pg_ctl.exe" -D $data status *> $null
if($LASTEXITCODE -ne 0){& "$pg/pg_ctl.exe" -D $data -l (Join-Path $state 'postgres.log') -o '-h 127.0.0.1 -p 55448' -w start; Check}
$dbs=@('flowcase_live','flowcase_tests','control_plane_tests','agent_tests','kms_tests','ucw_tests')
foreach($db in $dbs){
  $exists=& "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname='$db'"; Check
  if($exists -ne '1'){& "$pg/createdb.exe" -h 127.0.0.1 -p 55448 -U elenio_test $db; Check}
}
if($DatabaseOnly){Remove-Item Env:PGPASSWORD;Write-Output 'Disposable loopback databases ready.';exit 0}
$env:DATABASE_URL="postgres://elenio_test:$($env:PGPASSWORD)@127.0.0.1:55448/flowcase_live?sslmode=disable"
$env:APP_ENV='development';$env:AUTH_MODE='dev_mock';$env:DEV_AUTH_ACKNOWLEDGE='i-understand-this-is-not-secure'
$env:HTTP_ADDRESS='127.0.0.1:8080';$env:WORKER_HEALTH_ADDRESS='127.0.0.1:8081'
$env:DEV_AUTH_USER_ID='00000000-0000-4000-8000-000000000101';$env:DEV_AUTH_TENANT_ID='00000000-0000-4000-8000-000000000001'
$env:DEV_AUTH_DISPLAY_NAME='Local E2E developer';$env:DEV_AUTH_ROLES='workspace_member,case_owner,workflow_designer,workflow_publisher'
$env:DEV_AUTH_PERSONAS='[{"key":"approver","user_id":"00000000-0000-4000-8000-000000000102","name":"Local independent approver","roles":["workspace_member","domain_approver"]},{"key":"viewer","user_id":"00000000-0000-4000-8000-000000000103","name":"Local viewer","roles":["workspace_member"]}]'
# Real database/API; explicitly disabled external providers, never synthetic AI or ERP success.
$env:KMS_MODE='disabled';$env:AGENT_RUNTIME_MODE='disabled';$env:CONNECTORS_MODE='disabled';$env:PACKS_CONFIG='config/packs.json'
Push-Location (Join-Path $repo 'flowcase_platform')
try {
  & $go run ./cmd/migrate up; Check
  & $go run ./cmd/migrate seed-dev; Check
  foreach($component in @('api','worker')){
    $exe=Join-Path $state "flowcase-$component.exe"
    $pidFile=Join-Path $state "$component.pid"
    if(Test-Path $pidFile){
      $old=Get-Process -Id ([int](Get-Content $pidFile)) -ErrorAction SilentlyContinue
      if($old -and $old.Path -eq $exe){Write-Output "$component already running.";continue}
    }
    & $go build -o $exe "./cmd/$component"; Check
    $process=Start-Process -FilePath $exe -WorkingDirectory $PWD -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $state "$component.out.log") -RedirectStandardError (Join-Path $state "$component.err.log")
    Set-Content -LiteralPath $pidFile -Value $process.Id
  }
}finally{Pop-Location;Remove-Item Env:PGPASSWORD -ErrorAction SilentlyContinue;Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue}
Write-Output 'Local Flowcase API/worker started on loopback with explicit development identity. Check /readyz before testing.'
