param(
    [string]$WinUser
)

if ([string]::IsNullOrWhiteSpace($WinUser)) {
    $WinUser = $env:UserName
}

$profilesRoot = "C:\Users\$WinUser\AppData\Roaming\Elgato\StreamDeck\ProfilesV3"
if (-not (Test-Path -LiteralPath $profilesRoot)) {
    Write-Host "profiles root not found: $profilesRoot"
    exit 0
}

$updatedFiles = 0
$errorCount = 0

Get-ChildItem -LiteralPath $profilesRoot -Recurse -Filter manifest.json -File | ForEach-Object {
    $path = $_.FullName
    try {
        $raw = Get-Content -LiteralPath $path -Raw -ErrorAction Stop
        if ([string]::IsNullOrWhiteSpace($raw)) {
            return
        }

        $doc = $raw | ConvertFrom-Json -ErrorAction Stop
        $changed = $false

        foreach ($controller in @($doc.Controllers)) {
            if ($null -eq $controller -or $null -eq $controller.Actions) {
                continue
            }

            foreach ($entry in $controller.Actions.PSObject.Properties) {
                $action = $entry.Value
                if ($null -eq $action) {
                    continue
                }
                if ($action.UUID -ne "com.moeilijk.hwinfo.settings") {
                    continue
                }
                if ($null -eq $action.States) {
                    continue
                }

                foreach ($state in @($action.States)) {
                    if ($null -eq $state) {
                        continue
                    }
                    if ($state.TitleAlignment -ne "top") {
                        $state.TitleAlignment = "top"
                        $changed = $true
                    }
                    if ($state.FontSize -ne 9) {
                        $state.FontSize = 9
                        $changed = $true
                    }
                    $titleProp = $state.PSObject.Properties["Title"]
                    if ($null -eq $titleProp) {
                        $state | Add-Member -NotePropertyName Title -NotePropertyValue "Refresh Rate"
                        $changed = $true
                    }
                    elseif ([string]::IsNullOrWhiteSpace([string]$state.Title)) {
                        $state.Title = "Refresh Rate"
                        $changed = $true
                    }
                    if ($state.ShowTitle -ne $false) {
                        $state.ShowTitle = $false
                        $changed = $true
                    }
                }
            }
        }

        if ($changed) {
            $json = $doc | ConvertTo-Json -Depth 100 -Compress
            [System.IO.File]::WriteAllText($path, $json, [System.Text.UTF8Encoding]::new($false))
            $updatedFiles++
            Write-Host "updated profile manifest: $path"
        }
    }
    catch {
        $errorCount++
        Write-Warning ("failed to process {0}: {1}" -f $path, $_.Exception.Message)
    }
}

Write-Host "settings title state migration complete. updated_files=$updatedFiles errors=$errorCount"
