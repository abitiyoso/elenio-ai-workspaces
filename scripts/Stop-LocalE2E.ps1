param([Parameter(Mandatory)][string]$PostgresBin)
$ErrorActionPreference='Stop'
$repo=Split-Path $PSScriptRoot -Parent
$state=[IO.Path]::GetFullPath((Join-Path $repo '.local-e2e'))
foreach($name in @('api','worker')){
 $pidFile=Join-Path $state "$name.pid"
 if(Test-Path $pidFile){
  $process=Get-Process -Id ([int](Get-Content $pidFile)) -ErrorAction SilentlyContinue
  $expected=Join-Path $state "flowcase-$name.exe"
  if($process -and $process.Path -eq $expected){Stop-Process -Id $process.Id}
 }
}
$pg=(Resolve-Path $PostgresBin).Path
& "$pg/pg_ctl.exe" -D (Join-Path $state 'postgres-data') -m fast -w stop
if($LASTEXITCODE -ne 0){throw 'Local PostgreSQL stop failed'}
Write-Output 'Local sandbox stopped; its disposable data is retained.'
