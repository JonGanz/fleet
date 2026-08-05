# Recreates the tree rooted at -Src under -Dst: directories are created,
# regular files are hardlinked (New-Item -ItemType HardLink), and any actual
# reparse-point/symlink entries are recreated as symlinks rather than
# hardlinked -- mirroring fleet-cache/hardlink.go's Linux-side hardlinkTree,
# but as native Windows operations (Windows npm typically produces
# .cmd/.ps1/.js shim trios rather than real symlinks, so the symlink branch
# below is a defensive fallback, not the common case).
#
# -Src and -Dst must be on the same NTFS volume; New-Item -ItemType HardLink
# fails across volumes.
param(
    [Parameter(Mandatory = $true)][string]$Src,
    [Parameter(Mandatory = $true)][string]$Dst
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $Src -PathType Container)) {
    throw "source $Src is not a directory"
}

New-Item -ItemType Directory -Force -Path $Dst | Out-Null

Get-ChildItem -LiteralPath $Src -Recurse -Force | ForEach-Object {
    $rel = $_.FullName.Substring($Src.Length).TrimStart('\')
    $target = Join-Path $Dst $rel

    $isReparsePoint = $_.Attributes -band [System.IO.FileAttributes]::ReparsePoint

    if ($_.PSIsContainer -and -not $isReparsePoint) {
        New-Item -ItemType Directory -Force -Path $target | Out-Null
    } elseif ($isReparsePoint) {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target) | Out-Null
        if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Force -Recurse }
        $linkTarget = (Get-Item -LiteralPath $_.FullName).Target
        New-Item -ItemType SymbolicLink -Path $target -Target $linkTarget | Out-Null
    } else {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target) | Out-Null
        if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Force }
        New-Item -ItemType HardLink -Path $target -Value $_.FullName | Out-Null
    }
}
