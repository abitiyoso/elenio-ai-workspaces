param([Parameter(Mandatory)][string]$GoExe,[Parameter(Mandatory)][string]$PostgresBin)
$ErrorActionPreference='Stop'
$repo=Split-Path $PSScriptRoot -Parent
$state=Join-Path $repo '.local-e2e'
$private=Join-Path $state 'agent-private'
$go=(Resolve-Path -LiteralPath $GoExe).Path
$pg=(Resolve-Path -LiteralPath $PostgresBin).Path
$module=Join-Path $repo 'ai_agent_studio'
function Check {if($LASTEXITCODE -ne 0){throw 'Local Agent Studio command failed.'}}
function ProtectPath([string]$Path,[bool]$Directory){
 $sid=[Security.Principal.WindowsIdentity]::GetCurrent().User
 $existing=Get-Acl -LiteralPath $Path
 $rules=@($existing.Access)
 if($existing.AreAccessRulesProtected -and $existing.GetOwner([Security.Principal.SecurityIdentifier]).Value -eq $sid.Value -and $rules.Count -eq 1 -and $rules[0].IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value -eq $sid.Value -and $rules[0].AccessControlType -eq 'Allow' -and ($rules[0].FileSystemRights -band [Security.AccessControl.FileSystemRights]::FullControl) -eq [Security.AccessControl.FileSystemRights]::FullControl){return}
 if($Directory){$acl=[Security.AccessControl.DirectorySecurity]::new();$flags=[Security.AccessControl.InheritanceFlags]'ContainerInherit,ObjectInherit'}
 else{$acl=[Security.AccessControl.FileSecurity]::new();$flags=[Security.AccessControl.InheritanceFlags]::None}
 $acl.SetOwner($sid);$acl.SetAccessRuleProtection($true,$false)
 $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($sid,'FullControl',$flags,'None','Allow'))
 Set-Acl -LiteralPath $Path -AclObject $acl
}
[IO.Directory]::CreateDirectory($private)|Out-Null
ProtectPath $private $true
$master=Join-Path $private 'master.key'
if(!(Test-Path -LiteralPath $master)){[IO.File]::WriteAllBytes($master,[Security.Cryptography.RandomNumberGenerator]::GetBytes(32))}
ProtectPath $master $false
$access=Join-Path $private 'access.json'
# One explicit loopback-only development identity. No release review/decision permission.
$permissions=@('model_provider.read','model_provider.create','model_provider.credential.write','model_provider.credential.read','model_provider.probe','model_deployment.read','model_deployment.create','model_deployment.approve','model_deployment.approval.read','agent.read','agent.create','agent.version.read','agent.version.create','agent.model_binding.read','agent.model_binding.write','agent.skill.read','agent.tool.read','sandbox.run','run.read','run.cancel')
@(@{login='local-architect@elenio.local';tenant_id='00000000-0000-4000-8000-000000000001';permissions=$permissions})|ConvertTo-Json -Depth 5 -AsArray|Set-Content -LiteralPath $access
ProtectPath $access $false
$env:PGPASSWORD=[IO.File]::ReadAllText((Join-Path $state 'database-password'))
try {
 $exists=& "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname='agent_live'";Check
 if($exists -ne '1'){& "$pg/createdb.exe" -h 127.0.0.1 -p 55448 -U elenio_test agent_live;Check}
 & "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d agent_live -v ON_ERROR_STOP=1 -c "CREATE TABLE IF NOT EXISTS public.agent_studio_schema_migrations(version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())" > (Join-Path $state 'agent-live-migrate.log');Check
 foreach($sql in Get-ChildItem (Join-Path $module 'migrations') -Filter '*.up.sql'|Sort-Object Name){
  $version=$sql.Name -replace '\.up\.sql$',''
  if($version -notmatch '^\d{6}_[a-z0-9_]+$'){throw 'Invalid migration filename.'}
  $applied=& "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d agent_live -Atc "SELECT 1 FROM public.agent_studio_schema_migrations WHERE version='$version'";Check
  if($applied -ne '1'){
   & "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d agent_live -v ON_ERROR_STOP=1 -1 -f $sql.FullName -c "INSERT INTO public.agent_studio_schema_migrations(version) VALUES ('$version')" >> (Join-Path $state 'agent-live-migrate.log');Check
  }
 }

}finally{Remove-Item Env:PGPASSWORD -ErrorAction SilentlyContinue}
$env:AGENT_STUDIO_DATABASE_URL='postgres://elenio_test@127.0.0.1:55448/agent_live?sslmode=disable'
$env:AGENT_STUDIO_DATABASE_PASSWORD_FILE=Join-Path $state 'database-password'
$env:AGENT_STUDIO_ENV='development';$env:AGENT_STUDIO_DEV_LOGIN='local-architect@elenio.local'
$env:AGENT_STUDIO_ACCESS_MAP_FILE=$access;$env:AGENT_STUDIO_HTTP_ADDR='127.0.0.1:8190'
$env:AGENT_STUDIO_VAULT_DIR=Join-Path $private 'vault';$env:AGENT_STUDIO_VAULT_MASTER_KEY_FILE=$master
$env:AGENT_STUDIO_PROVIDER_HTTPS_HOSTS='9router.elenio.id';$env:AGENT_STUDIO_WORKER_CONCURRENCY='1'
$env:AGENT_STUDIO_WEB_DIR=Join-Path $module 'apps/agent-studio-web/dist'
if(!(Test-Path -LiteralPath (Join-Path $env:AGENT_STUDIO_WEB_DIR 'index.html'))){throw 'Build ai_agent_studio/apps/agent-studio-web before starting the local service.'}
$env:AGENT_STUDIO_DEV_MOCK_MODEL='false'
$exe=Join-Path $state 'agent-studio.exe'
$pidFile=Join-Path $state 'agent.pid'
if(Test-Path -LiteralPath $pidFile){$running=Get-Process -Id ([int](Get-Content -LiteralPath $pidFile)) -ErrorAction SilentlyContinue;if($running -and $running.Path -eq $exe){Write-Output 'Local Agent Studio already running.';exit 0}}
Push-Location $module
try{& $go build -o $exe ./cmd/agent-studio;Check}finally{Pop-Location}
$process=Start-Process -FilePath $exe -WorkingDirectory $module -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $state 'agent.out.log') -RedirectStandardError (Join-Path $state 'agent.err.log')
Set-Content -LiteralPath $pidFile -Value $process.Id
$ready=$false
for($attempt=0;$attempt -lt 20;$attempt++){
 try{$check=Invoke-WebRequest 'http://127.0.0.1:8190/health/ready' -TimeoutSec 2;if($check.StatusCode -eq 200){$ready=$true;break}}catch{}
 if($process.HasExited){throw 'Local Agent Studio stopped before readiness; inspect the private agent.err.log.'}
 Start-Sleep -Milliseconds 500
}
if(!$ready){throw 'Local Agent Studio did not become ready.'}
Write-Output 'Local Agent Studio ready on 127.0.0.1:8190; separate disposable database, owner-only encrypted vault, no production releases.'
