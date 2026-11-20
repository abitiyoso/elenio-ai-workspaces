param([Parameter(Mandatory)][string]$PostgresBin)
$ErrorActionPreference='Stop'
$repo=Split-Path $PSScriptRoot -Parent
$state=[IO.Path]::GetFullPath((Join-Path $repo '.local-e2e'))
$owned=@{'api'='flowcase-api.exe';'worker'='flowcase-worker.exe';'agent'='agent-studio.exe';'workspace-control'='control-plane.exe';'workspace-identity'='local-workspace.exe';'workspace-gateway'='local-workspace.exe';'workspace-kms-gateway'='local-workspace.exe';'workspace-api'='workspace-flowcase-api.exe';'workspace-worker'='workspace-flowcase-worker.exe';'workspace-agent'='workspace-agent-studio.exe';'workspace-kms'='workspace-kms.exe';'workspace-ucw'='workspace-ucw.exe'}
foreach($name in $owned.Keys){
 $pidFile=Join-Path $state "$name.pid"
 if(Test-Path $pidFile){
  $process=Get-Process -Id ([int](Get-Content $pidFile)) -ErrorAction SilentlyContinue
  $expected=Join-Path $state $owned[$name]
  if($process -and $process.Path -eq $expected){Stop-Process -Id $process.Id}
 }
}
$pg=(Resolve-Path $PostgresBin).Path
& "$pg/pg_ctl.exe" -D (Join-Path $state 'postgres-data') -m fast -w stop
if($LASTEXITCODE -ne 0){throw 'Local PostgreSQL stop failed'}
Write-Output 'Local sandbox stopped; its disposable data is retained.'
