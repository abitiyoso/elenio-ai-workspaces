param([Parameter(Mandatory)][string]$GoExe,[Parameter(Mandatory)][string]$PostgresBin,[switch]$Restart)
$ErrorActionPreference='Stop'
$repo=Split-Path $PSScriptRoot -Parent
$state=[IO.Path]::GetFullPath((Join-Path $repo '.local-e2e'))
$private=Join-Path $state 'workspace-private'
$go=(Resolve-Path -LiteralPath $GoExe).Path;$pg=(Resolve-Path -LiteralPath $PostgresBin).Path
function Check {if($LASTEXITCODE -ne 0){throw 'Local workspace command failed; inspect ignored local logs.'}}
function Protect([string]$Path){
 $sid=[Security.Principal.WindowsIdentity]::GetCurrent().User
 $acl=Get-Acl -LiteralPath $Path;$rules=@($acl.Access)
 if($acl.AreAccessRulesProtected -and $acl.GetOwner([Security.Principal.SecurityIdentifier]).Value -eq $sid.Value -and $rules.Count -eq 1 -and $rules[0].IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value -eq $sid.Value -and $rules[0].AccessControlType -eq 'Allow' -and ($rules[0].FileSystemRights -band [Security.AccessControl.FileSystemRights]::FullControl) -eq [Security.AccessControl.FileSystemRights]::FullControl){return}
 $acl=[Security.AccessControl.DirectorySecurity]::new();$acl.SetOwner($sid);$acl.SetAccessRuleProtection($true,$false);$acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new($sid,'FullControl','ContainerInherit,ObjectInherit','None','Allow'));Set-Acl -LiteralPath $Path -AclObject $acl
}
function StopOwned([string]$Name,[string]$Executable){
 $pidFile=Join-Path $state "$Name.pid"
 if(Test-Path -LiteralPath $pidFile){$process=Get-Process -Id ([int](Get-Content -LiteralPath $pidFile)) -ErrorAction SilentlyContinue;if($process -and $process.Path -eq $Executable){Stop-Process -Id $process.Id; $process.WaitForExit(5000)|Out-Null}}
}
function StartOwned([string]$Name,[string]$Executable,[string[]]$Arguments,[string]$WorkingDirectory){
 $pidFile=Join-Path $state "$Name.pid"
 if(Test-Path -LiteralPath $pidFile){$existing=Get-Process -Id ([int](Get-Content -LiteralPath $pidFile)) -ErrorAction SilentlyContinue;if($existing -and $existing.Path -eq $Executable){return}}
 $parameters=@{FilePath=$Executable;WorkingDirectory=$WorkingDirectory;WindowStyle='Hidden';PassThru=$true;RedirectStandardOutput=(Join-Path $private "$Name.out.log");RedirectStandardError=(Join-Path $private "$Name.err.log")}
 if($Arguments.Count){$parameters.ArgumentList=$Arguments}
 $process=Start-Process @parameters;Set-Content -LiteralPath $pidFile -Value $process.Id
}
function Ready([string]$URL){
 for($attempt=0;$attempt -lt 35;$attempt++){try{$result=Invoke-WebRequest $URL -TimeoutSec 2;if($result.StatusCode -eq 200){return}}catch{};Start-Sleep -Milliseconds 400};throw "Local service readiness failed: $URL. Inspect private logs."
}
function Sql([string]$Database,[string]$Statement){
 if($Database -notin @('postgres','control_plane_workspace_live','flowcase_workspace_live')){throw 'Unexpected sandbox database'}
 $result=& "$pg/psql.exe" -h 127.0.0.1 -p 55448 -U elenio_test -d $Database -v ON_ERROR_STOP=1 -Atq -c $Statement;Check;return $result
}
function Literal([string]$Value){return "'"+$Value.Replace("'","''")+"'"}
& (Join-Path $PSScriptRoot 'Start-LocalE2E.ps1') -GoExe $go -PostgresBin $pg -DatabaseOnly;Check
[IO.Directory]::CreateDirectory($private)|Out-Null;Protect $private
foreach($file in @('oidc-client','csrf-key','token-encryption-key')){$path=Join-Path $private $file;if(!(Test-Path -LiteralPath $path)){[IO.File]::WriteAllText($path,[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)))}}
$executables=@{'workspace-identity'=(Join-Path $state 'local-workspace.exe');'workspace-gateway'=(Join-Path $state 'local-workspace.exe');'workspace-control'=(Join-Path $state 'control-plane.exe');'workspace-api'=(Join-Path $state 'workspace-flowcase-api.exe');'workspace-worker'=(Join-Path $state 'workspace-flowcase-worker.exe');'workspace-agent'=(Join-Path $state 'workspace-agent-studio.exe')}
if($Restart){foreach($name in $executables.Keys){StopOwned $name $executables[$name]}}
# Rebuild only when owned services are stopped; never overwrite a running executable.
$running=Get-Process -Name local-workspace,control-plane,workspace-flowcase-api,workspace-flowcase-worker,workspace-agent-studio -ErrorAction SilentlyContinue
if($running){throw 'Workspace services already running. Use -Restart to apply source/config updates.'}
$env:ELENIO_LOCAL_E2E='1';$env:APP_ENV='development';$env:PGPASSWORD=[IO.File]::ReadAllText((Join-Path $state 'database-password'))
try{
 foreach($db in @('control_plane_workspace_live','flowcase_workspace_live')){if((Sql postgres "SELECT 1 FROM pg_database WHERE datname='$db'") -ne '1'){& "$pg/createdb.exe" -h 127.0.0.1 -p 55448 -U elenio_test $db;Check}}
 Push-Location (Join-Path $repo 'control_plane')
 try{
  & $go build -o $executables['workspace-control'] ./cmd/control-plane-api;Check
  & $go build -o $executables['workspace-identity'] ./tools/local-workspace;Check
  $signing=Join-Path $private 'cp-signing-key'
  if(!(Test-Path -LiteralPath $signing)){$key=& $executables['workspace-control'] gen-signing-key;Check;[IO.File]::WriteAllText($signing,($key -join "`n")+"`n");$key=$null}
 }finally{Pop-Location}
 StartOwned 'workspace-identity' $executables['workspace-identity'] @('-mode=identity',"-client-secret-file=$private/oidc-client","-seed-file=$repo/control_plane/deploy/dev-seed.json") $repo
 Ready 'http://127.0.0.1:8210/healthz'
 # Persistent server session keys; only synthetic seed identities in this isolated database.
 $cpDatabase=Join-Path $private 'cp-database-url'
 [IO.File]::WriteAllText($cpDatabase,"postgres://elenio_test:$($env:PGPASSWORD)@127.0.0.1:55448/control_plane_workspace_live?sslmode=disable")
 $env:DATABASE_URL='';$env:DATABASE_URL_FILE=$cpDatabase;$env:APP_ENV='development';$env:PUBLIC_BASE_URL='http://127.0.0.1:8200'
 $env:HTTP_ADDRESS='127.0.0.1:8200';$env:METRICS_ADDRESS='127.0.0.1:9200';$env:TENANT_KEY='acme-demo';$env:TENANT_NAME='Elenio local workspace'
 $env:OIDC_ISSUER_URL='http://127.0.0.1:8210';$env:OIDC_CLIENT_ID='control-plane';$env:OIDC_CLIENT_SECRET_FILE=Join-Path $private 'oidc-client'
 $env:CSRF_HMAC_KEY_FILE=Join-Path $private 'csrf-key';$env:TOKEN_ENCRYPTION_KEY_FILE=Join-Path $private 'token-encryption-key';$env:SERVICE_TOKEN_SIGNING_KEY_FILE=$signing
 $env:COOKIE_INSECURE_DEV_ACKNOWLEDGE='i-understand-cookies-are-not-secure';$env:DEV_SEED='true';$env:DEV_SEED_FILE=Join-Path $repo 'control_plane/deploy/dev-seed.json'
 $env:WEB_DIR=Join-Path $repo 'control_plane/web/dist'
 if(!(Test-Path -LiteralPath (Join-Path $env:WEB_DIR 'index.html'))){throw 'Build control_plane/web before starting the workspace.'}
 $env:TRUSTED_APP_UPSTREAMS=@{flowcase=@{url='http://127.0.0.1:5280';audience='flowcase';mode='proxy';strip_prefix=$true;health_url='http://127.0.0.1:5280/readyz'};'agent-studio'=@{url='http://127.0.0.1:8290';audience='agent-studio';mode='proxy';strip_prefix=$true;health_url='http://127.0.0.1:8290/health/ready'};kms=@{mode='pending';pending_reason='KMS native workspace adapter is not configured.'};'case-workspace'=@{mode='pending';pending_reason='UCW workspace adapter is not configured.'}}|ConvertTo-Json -Depth 8 -Compress
 StartOwned 'workspace-control' $executables['workspace-control'] @('serve') (Join-Path $repo 'control_plane')
 Ready 'http://127.0.0.1:8200/readyz'
 $cpUsers=@((Sql control_plane_workspace_live "SELECT COALESCE(json_agg(json_build_object('subject',idp_subject,'id',id,'name',display_name)),'[]') FROM users")|ConvertFrom-Json)
 # Flowcase domain grants are explicitly sourced from its existing directory fixture, never CP roles.
 $env:DATABASE_URL="postgres://elenio_test:$($env:PGPASSWORD)@127.0.0.1:55448/flowcase_workspace_live?sslmode=disable"
 $env:APP_ENV='development';$env:AUTH_MODE='dev_mock';$env:DEV_AUTH_ACKNOWLEDGE='i-understand-this-is-not-secure'
 $env:DEV_AUTH_USER_ID='00000000-0000-4000-8000-000000000101';$env:DEV_AUTH_TENANT_ID='00000000-0000-4000-8000-000000000001';$env:DEV_AUTH_DISPLAY_NAME='Local fixture importer';$env:DEV_AUTH_ROLES='workspace_member,case_owner,workflow_designer,workflow_publisher';$env:DEV_AUTH_PERSONAS='[{"key":"approver","user_id":"00000000-0000-4000-8000-000000000102","name":"Local independent approver","roles":["workspace_member","domain_approver"]}]'
 $env:KMS_MODE='disabled';$env:AGENT_RUNTIME_MODE='disabled';$env:CONNECTORS_MODE='disabled';$env:PACKS_CONFIG='config/packs.json'
 Push-Location (Join-Path $repo 'flowcase_platform');try{& $go run ./cmd/migrate up > (Join-Path $private 'flowcase-migrate.log');Check;& $go run ./cmd/migrate seed-dev > (Join-Path $private 'flowcase-seed.log');Check;& $go build -o $executables['workspace-api'] ./cmd/api;Check;& $go build -o $executables['workspace-worker'] ./cmd/worker;Check}finally{Pop-Location}
 $directory=Get-Content (Join-Path $repo 'control_plane/deploy/flowcase-cp/directory.json') -Raw|ConvertFrom-Json
 $tenant="(SELECT id FROM tenants WHERE external_ref='acme-demo')"
 $statements=[Collections.Generic.List[string]]::new();$statements.Add('BEGIN;');$statements.Add("UPDATE tenants SET external_ref='acme-demo',name='Elenio local workspace' WHERE id='00000000-0000-4000-8000-000000000001';")
 foreach($entry in $directory.users){$user=@($cpUsers|Where-Object subject -eq $entry.subject);if($user.Count -ne 1){throw 'Existing domain directory subject missing from CP'};$uid=Literal $user[0].id;$name=Literal $user[0].name;$member="(SELECT id FROM users WHERE issuer='http://127.0.0.1:8200' AND subject=$uid)";$statements.Add("INSERT INTO users(issuer,subject,display_name) VALUES('http://127.0.0.1:8200',$uid,$name) ON CONFLICT(issuer,subject) DO NOTHING; INSERT INTO memberships(tenant_id,user_id) VALUES($tenant,$member) ON CONFLICT DO NOTHING;")
  foreach($role in $entry.roles){$value=Literal $role;$statements.Add("INSERT INTO role_assignments(tenant_id,user_id,role) VALUES($tenant,$member,$value) ON CONFLICT DO NOTHING;")}
  if($entry.copy_domain_roles_from){$source=Literal $entry.copy_domain_roles_from;$statements.Add("INSERT INTO domain_role_assignments(tenant_id,user_id,role) SELECT s.tenant_id,$member,s.role FROM domain_role_assignments s WHERE s.user_id=${source}::uuid AND s.revoked_at IS NULL AND s.case_id IS NULL AND NOT EXISTS(SELECT 1 FROM domain_role_assignments d WHERE d.user_id=$member AND d.role=s.role AND d.revoked_at IS NULL AND d.case_id IS NULL);")}
  if($entry.copy_delegations_from){$source=Literal $entry.copy_delegations_from;$statements.Add("INSERT INTO policy_delegations(tenant_id,policy_ref,user_id) SELECT s.tenant_id,s.policy_ref,$member FROM policy_delegations s WHERE s.user_id=${source}::uuid AND s.revoked_at IS NULL AND NOT EXISTS(SELECT 1 FROM policy_delegations d WHERE d.user_id=$member AND d.policy_ref=s.policy_ref AND d.revoked_at IS NULL);")}
 }
 $statements.Add('COMMIT;');$null=Sql flowcase_workspace_live ($statements -join "`n")
 $env:AUTH_MODE='trusted_jwt';$env:AUTH_ISSUER='http://127.0.0.1:8200';$env:AUTH_AUDIENCE='flowcase';$env:AUTH_JWKS_URL='http://127.0.0.1:8200/.well-known/jwks.json'
 $env:DEV_AUTH_PERSONAS='';$env:SELF_APPROVAL_EXEMPT_SUBJECTS='';$env:HTTP_ADDRESS='127.0.0.1:8280';$env:WORKER_HEALTH_ADDRESS='127.0.0.1:8281';$env:METRICS_ADDRESS='127.0.0.1:9280'
 StartOwned 'workspace-api' $executables['workspace-api'] @() (Join-Path $repo 'flowcase_platform');StartOwned 'workspace-worker' $executables['workspace-worker'] @() (Join-Path $repo 'flowcase_platform');Ready 'http://127.0.0.1:8280/readyz'
 Remove-Item Env:DATABASE_URL,Env:PGPASSWORD -ErrorAction SilentlyContinue
 # Separate mounted web build leaves the existing standalone preview untouched.
 $web=Join-Path $state 'workspace-flowcase-web';$env:FLOWCASE_BASE_PATH='/apps/flowcase/'
 Push-Location (Join-Path $repo 'flowcase_platform/web');try{& npm run build -- --outDir $web > (Join-Path $private 'flowcase-web-build.log');Check}finally{Pop-Location;Remove-Item Env:FLOWCASE_BASE_PATH -ErrorAction SilentlyContinue}
 StartOwned 'workspace-gateway' $executables['workspace-gateway'] @('-mode=gateway',"-web-dir=$web") $repo
 Ready 'http://127.0.0.1:5280/readyz'
 # Reuse the existing Agent Studio database/vault and independent source-defined grants.
 if(!(Test-Path -LiteralPath (Join-Path $state 'agent-private/master.key'))){throw 'Start/configure the existing local Agent Studio first.'}
 $agentDirectory=Get-Content (Join-Path $repo 'control_plane/deploy/agent-studio-cp/directory.json') -Raw|ConvertFrom-Json
 $access=@();foreach($entry in $agentDirectory.users){$user=@($cpUsers|Where-Object subject -eq $entry.subject);if($user.Count -eq 1){$access+=@{login=$user[0].id;tenant_id='00000000-0000-4000-8000-000000000001';permissions=$entry.permissions}}}
 $access|ConvertTo-Json -Depth 10 -AsArray|Set-Content -LiteralPath (Join-Path $private 'agent-access.json')
 $env:AGENT_STUDIO_WEB_BASE_PATH='/apps/agent-studio/';$agentWeb=Join-Path $state 'workspace-agent-web'
 Push-Location (Join-Path $repo 'ai_agent_studio/apps/agent-studio-web');try{& npm run build -- --outDir $agentWeb > (Join-Path $private 'agent-web-build.log');Check}finally{Pop-Location;Remove-Item Env:AGENT_STUDIO_WEB_BASE_PATH -ErrorAction SilentlyContinue}
 Push-Location (Join-Path $repo 'ai_agent_studio');try{& $go build -o $executables['workspace-agent'] ./cmd/agent-studio;Check}finally{Pop-Location}
 $env:AGENT_STUDIO_ENV='development';$env:AGENT_STUDIO_HUMAN_AUTH='control_plane_jwt';$env:AGENT_STUDIO_DEV_LOGIN='';$env:AGENT_STUDIO_CP_ISSUER='http://127.0.0.1:8200';$env:AGENT_STUDIO_CP_JWKS_URL='http://127.0.0.1:8200/.well-known/jwks.json';$env:AGENT_STUDIO_CP_TENANT_KEY='acme-demo';$env:AGENT_STUDIO_CP_AUDIENCE='agent-studio';$env:AGENT_STUDIO_ACCESS_MAP_FILE=Join-Path $private 'agent-access.json'
 $env:AGENT_STUDIO_DATABASE_URL='postgres://elenio_test@127.0.0.1:55448/agent_live?sslmode=disable';$env:AGENT_STUDIO_DATABASE_PASSWORD_FILE=Join-Path $state 'database-password';$env:AGENT_STUDIO_HTTP_ADDR='127.0.0.1:8290';$env:AGENT_STUDIO_WEB_DIR=$agentWeb
 $env:AGENT_STUDIO_VAULT_DIR=Join-Path $state 'agent-private/vault';$env:AGENT_STUDIO_VAULT_MASTER_KEY_FILE=Join-Path $state 'agent-private/master.key';$env:AGENT_STUDIO_PROVIDER_HTTPS_HOSTS='9router.elenio.id';$env:AGENT_STUDIO_DEV_MOCK_MODEL='false';$env:AGENT_STUDIO_WORKER_CONCURRENCY='1';$env:AGENT_STUDIO_SELF_APPROVAL_EXEMPT_SUBJECTS=''
 StartOwned 'workspace-agent' $executables['workspace-agent'] @() (Join-Path $repo 'ai_agent_studio');Ready 'http://127.0.0.1:8290/health/ready'
 Write-Output 'Local workspace ready: http://127.0.0.1:8200/apps/flowcase/elenio.html . Explicit local OIDC identities, real CP/Flowcase/Agent services; no production identity or deployment.'
}finally{Remove-Item Env:PGPASSWORD,Env:DATABASE_URL -ErrorAction SilentlyContinue}
