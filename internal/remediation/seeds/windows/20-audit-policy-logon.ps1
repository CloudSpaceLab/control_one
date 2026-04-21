# CIS Windows 17.5.x — Ensure Logon/Logoff audit policy is Success and Failure
# SOC2 CC7.2 — Record authentication events
$ErrorActionPreference = 'Stop'

$subcategories = @('Logon','Logoff','Account Lockout','Special Logon','Other Logon/Logoff Events')

foreach ($sub in $subcategories) {
    $proc = Start-Process -FilePath 'auditpol.exe' `
        -ArgumentList '/set', "/subcategory:`"$sub`"", '/success:enable', '/failure:enable' `
        -Wait -PassThru -WindowStyle Hidden
    if ($proc.ExitCode -ne 0) {
        throw "auditpol /set for $sub failed with $($proc.ExitCode)"
    }
}

# Force Advanced Audit Policy to override legacy policy precedence (CIS 2.3.2.1).
$regPath = 'HKLM:\SYSTEM\CurrentControlSet\Control\Lsa'
Set-ItemProperty -Path $regPath -Name SCENoApplyLegacyAuditPolicy -Value 1 -Type DWord

Write-Output 'Logon/Logoff audit policy: Success + Failure for all relevant subcategories'
