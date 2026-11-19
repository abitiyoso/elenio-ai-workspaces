# Provision only the explicitly authorized local development provider and tool-free draft agent.
# This does not approve an agent release or any staging/production deployment.
$ErrorActionPreference='Stop'
$repo=Split-Path $PSScriptRoot -Parent
$base='http://127.0.0.1:8190/api/v1'
function Agent([string]$Method,[string]$Path,$Body=$null){
 $options=@{Method=$Method;Uri="$base$Path";ContentType='application/json';TimeoutSec=100}
 if($null -ne $Body){$options.Body=($Body|ConvertTo-Json -Depth 70 -Compress)}
 try{Invoke-RestMethod @options}catch{throw "Local Agent Studio request failed: $Method $Path. Provider bodies and credentials are not logged."}
}
function Canonical($Value){
 if($Value -is [System.Collections.IDictionary]){$ordered=[ordered]@{};foreach($key in ($Value.Keys|Sort-Object)){$ordered[$key]=Canonical $Value[$key]};return $ordered}
 if($Value -is [System.Collections.IEnumerable] -and $Value -isnot [string]){return ,@($Value|ForEach-Object {Canonical $_})}
 return $Value
}
$session=Agent GET '/session'
if($session.identity_mode -ne 'development' -or $session.tenant_id -ne '00000000-0000-4000-8000-000000000001'){throw 'Expected explicit disposable development session.'}
$provider=(Agent GET '/model-providers?limit=100').items|Where-Object key -eq 'elenio-nine-router'
if(!$provider){$provider=Agent POST '/model-providers' @{key='elenio-nine-router';display_name='9Router Elenio';adapter_type='openai_compatible';base_url='https://9router.elenio.id/v1'}}
if($provider.base_url -ne 'https://9router.elenio.id/v1' -or $provider.adapter_type -ne 'openai_compatible'){throw 'Existing provider configuration does not match the authorized destination.'}
$credentialStatus=Agent GET "/model-providers/$($provider.id)/credentials"
if(!$credentialStatus.credential_configured){
 $envFile=Join-Path $repo 'ai_agent_studio/.env.local'
 $values=@([IO.File]::ReadLines($envFile)|Where-Object {$_ -match '^\s*NINE_ROUTER_API_KEY\s*='})
 if($values.Count -ne 1){throw 'Expected exactly one local server credential variable.'}
 $credential=($values[0] -replace '^\s*NINE_ROUTER_API_KEY\s*=\s*','').Trim().Trim('"',"'")
 if(!$credential -or $credential.IndexOfAny([char[]]"`r`n`0") -ge 0){throw 'Invalid server credential.'}
 try{$null=Agent PUT "/model-providers/$($provider.id)/credentials" @{credential=$credential}}finally{$credential=$null;$values=$null}
}
$deployment=(Agent GET '/model-deployments?limit=100').items|Where-Object key -eq 'elenio-luna-authoring'
if(!$deployment){$deployment=Agent POST '/model-deployments' @{provider_id=$provider.id;key='elenio-luna-authoring';provider_model_id='cx/gpt-6-luna';display_name='Elenio Luna · draft authoring';capabilities=@{json=$true;tools=$false};max_output_tokens=4096;timeout_ms=100000;cost_policy=@{}}}
if($deployment.provider_id -ne $provider.id -or $deployment.provider_model_id -ne 'cx/gpt-6-luna'){throw 'Existing deployment does not match the authorized model.'}
$approval=@(Agent GET "/model-deployments/$($deployment.id)/approvals?environment=development")|Where-Object state -eq 'approved'
if(!$approval){
 $probe=Agent POST "/model-providers/$($provider.id)/probe" @{deployment_id=$deployment.id;mode='capabilities';capabilities=@{json=$true;tools=$false};confirm_token_cost=$true}
 if($probe.status -ne 'active'){throw 'Real provider capability probe failed.'}
 $null=Agent POST "/model-deployments/$($deployment.id)/approvals?environment=development" @{state='approved';reason='Owner explicitly authorized cx/gpt-6-luna on 9Router for local application integration and synthetic testing. Development sandbox only.'}
}
$pack=Get-Content (Join-Path $repo 'ai_agent_studio/contracts/architect_agent.json') -Raw|ConvertFrom-Json -AsHashtable
$agent=(Agent GET '/agents?limit=100').items|Where-Object key -eq $pack.agent.key
if(!$agent){$agent=Agent POST '/agents' $pack.agent}
$versions=(Agent GET "/catalog/agents/$($pack.agent.key)/versions?limit=100").items
$version=$versions|Where-Object semver -eq $pack.version.semver
if(!$version){$version=Agent POST "/catalog/agents/$($pack.agent.key)/versions" $pack.version}
# Never overwrite an existing authoring version silently, including draft content.
$actual=$version|ConvertTo-Json -Depth 70|ConvertFrom-Json -AsHashtable
foreach($field in @('manifest','input_schema','output_schema','model_policy','tool_policy')){
 if((Canonical $actual[$field]|ConvertTo-Json -Depth 70 -Compress) -ne (Canonical $pack.version[$field]|ConvertTo-Json -Depth 70 -Compress)){throw 'Existing Architect version differs from the supplied contract; create a reviewed new version.'}
}
$null=Agent PUT "/agent-versions/$($version.id)/model-binding" @{model_deployment_id=$deployment.id;environment='development';override_policy=@{}}
[ordered]@{provider='9Router Elenio';model='cx/gpt-6-luna';environment='development';agent_key=$pack.agent.key;agent_version_id=$version.id;credential_in_encrypted_vault=$true;agent_release_created=$false}|ConvertTo-Json
