# First-logon script for the Defender test VM (scripts/defender-vm.sh, issue
# #116). Runs once, elevated, as the local "scan" account right after the
# unattended installation. It installs the OpenSSH server so the host can run
# PowerShell in the VM over ssh with the key in @@PUBKEY@@, and switches off
# sleep so the VM stays reachable. Everything is logged to C:\avscan.
$ErrorActionPreference = 'Continue'
New-Item -ItemType Directory -Force -Path 'C:\avscan' | Out-Null
Start-Transcript -Path 'C:\avscan\firstlogon.log' -Append | Out-Null

powercfg /change standby-timeout-ac 0
powercfg /change monitor-timeout-ac 0
powercfg /hibernate off

# OpenSSH server is a Windows capability; it needs Windows Update reachable.
$cap = Get-WindowsCapability -Online | Where-Object Name -like 'OpenSSH.Server*' | Select-Object -First 1
for ($i = 0; $i -lt 20 -and $cap.State -ne 'Installed'; $i++) {
  try { Add-WindowsCapability -Online -Name $cap.Name | Out-Null } catch { Write-Output "Add-WindowsCapability failed: $_" }
  $cap = Get-WindowsCapability -Online -Name $cap.Name
  if ($cap.State -ne 'Installed') { Start-Sleep -Seconds 30 }
}
Write-Output "OpenSSH.Server state: $($cap.State)"

New-Item -Path 'HKLM:\SOFTWARE\OpenSSH' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SOFTWARE\OpenSSH' -Name 'DefaultShell' `
  -Value "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -PropertyType String -Force | Out-Null

New-Item -ItemType Directory -Force -Path 'C:\ProgramData\ssh' | Out-Null
$ak = 'C:\ProgramData\ssh\administrators_authorized_keys'
Set-Content -Path $ak -Value '@@PUBKEY@@' -Encoding ascii
icacls $ak /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' | Out-Null

Set-Service -Name sshd -StartupType Automatic
Start-Service -Name sshd
# The capability's own firewall rule only covers the Private profile, while
# the QEMU NAT network is classified Public; open it for every profile.
if (-not (Get-NetFirewallRule -Name 'OpenSSH-Server-In-TCP' -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule -Name 'OpenSSH-Server-In-TCP' -DisplayName 'OpenSSH Server (sshd)' -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22 -Profile Any | Out-Null
}
Set-NetFirewallRule -Name 'OpenSSH-Server-In-TCP' -Enabled True -Profile Any
Write-Output "sshd: $((Get-Service sshd).Status)"
Set-Content -Path 'C:\avscan\firstlogon.done' -Value (Get-Date -Format o)
Stop-Transcript | Out-Null
