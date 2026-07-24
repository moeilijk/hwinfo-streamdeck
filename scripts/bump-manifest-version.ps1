$manifest = "com.exension.hwinfo.sdPlugin/manifest.json"
$state = ".last_patch"
$content = Get-Content -Raw $manifest
$pattern = '"Version"\s*:\s*"(?<major>\d+)\.(?<minor>\d+)\.(?<patch>\d+)\.(?<build>\d+)"'

if ($content -notmatch $pattern) {
  Write-Error "Version not found or invalid format in manifest.json"
  exit 1
}

$patchKey = "$($Matches["major"]).$($Matches["minor"]).$($Matches["patch"])"
$build = [int]$Matches["build"]
$lastPatch = ""
if (Test-Path $state) {
  $lastPatch = (Get-Content -Raw $state).Trim()
}

if ($patchKey -ne $lastPatch) {
  $newVersion = "$patchKey.0"
  Set-Content -Path $state -Value $patchKey -Encoding utf8
  $content = [regex]::Replace($content, $pattern, "\"Version\": \"$newVersion\"", 1)
  Set-Content -Path $manifest -Value $content -Encoding utf8
  Write-Output $newVersion
  exit 0
}

$newVersion = "$patchKey.$($build + 1)"
$content = [regex]::Replace($content, $pattern, "\"Version\": \"$newVersion\"", 1)
Set-Content -Path $manifest -Value $content -Encoding utf8
Write-Output $newVersion
