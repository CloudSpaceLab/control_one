# Rollback — disable Logon/Logoff auditing.
$ErrorActionPreference = 'Stop'

$subcategories = @('Logon','Logoff','Account Lockout','Special Logon','Other Logon/Logoff Events')
foreach ($sub in $subcategories) {
    Start-Process -FilePath 'auditpol.exe' `
        -ArgumentList '/set', "/subcategory:`"$sub`"", '/success:disable', '/failure:disable' `
        -Wait -WindowStyle Hidden | Out-Null
}

Write-Warning 'Logon/Logoff auditing disabled by rollback.'
