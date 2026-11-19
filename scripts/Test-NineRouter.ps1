param(
  [Parameter(Mandatory)][string]$GoExe,
  [switch]$Runtime
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
$module = Join-Path $repo 'ai_agent_studio'
$envFile = Join-Path $module '.env.local'
$go = (Resolve-Path -LiteralPath $GoExe).Path
if (!(Test-Path -LiteralPath $envFile -PathType Leaf)) { throw 'Server credential file is missing.' }
Push-Location $repo
try {
  & git check-ignore --quiet -- ai_agent_studio/.env.local
  if ($LASTEXITCODE -ne 0) { throw 'Server credential file must be ignored by Git.' }
} finally { Pop-Location }
# Read only the named variable; do not import unrelated configuration or print it.
$values = @([IO.File]::ReadLines($envFile) | Where-Object { $_ -match '^\s*NINE_ROUTER_API_KEY\s*=' })
if ($values.Count -ne 1) { throw 'Expected exactly one server credential variable.' }
$credential = ($values[0] -replace '^\s*NINE_ROUTER_API_KEY\s*=\s*', '').Trim()
if ($credential.Length -ge 2 -and (($credential.StartsWith('"') -and $credential.EndsWith('"')) -or ($credential.StartsWith("'") -and $credential.EndsWith("'")))) {
  $credential = $credential.Substring(1, $credential.Length - 2)
}
if (!$credential -or $credential.IndexOfAny([char[]]"`r`n`0") -ge 0) { throw 'Invalid server credential.' }
$previous = [Environment]::GetEnvironmentVariable('NINE_ROUTER_API_KEY', 'Process')
Push-Location $module
try {
  $env:NINE_ROUTER_API_KEY = $credential
  if ($Runtime) { & $go run ./tools/provider-check -runtime } else { & $go run ./tools/provider-check }
  if ($LASTEXITCODE -ne 0) { throw '9Router diagnostic failed; inspect the sanitized code above.' }
} finally {
  [Environment]::SetEnvironmentVariable('NINE_ROUTER_API_KEY', $previous, 'Process')
  $credential = $null
  $values = $null
  Pop-Location
}
