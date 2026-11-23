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

# Walks one directory level at a time instead of Get-ChildItem -Recurse:
# under Windows PowerShell 5.1, -Recurse descends into directory symlinks and
# junctions, which would link the *contents* of whatever they point at.
# Reparse points are recreated as symlinks and never descended into.
function Link-Tree {
    param([string]$SrcDir, [string]$DstDir)

    New-Item -ItemType Directory -Force -Path $DstDir | Out-Null

    foreach ($item in Get-ChildItem -LiteralPath $SrcDir -Force) {
        $target = Join-Path $DstDir $item.Name
        $isReparsePoint = $item.Attributes -band [System.IO.FileAttributes]::ReparsePoint

        if ($isReparsePoint) {
            if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Force -Recurse }
            $linkTarget = $item.Target
            if ($linkTarget -is [array]) { $linkTarget = $linkTarget[0] }
            New-Item -ItemType SymbolicLink -Path $target -Target $linkTarget | Out-Null
        } elseif ($item.PSIsContainer) {
            Link-Tree -SrcDir $item.FullName -DstDir $target
        } else {
            if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Force }
            New-Item -ItemType HardLink -Path $target -Value $item.FullName | Out-Null
        }
    }
}

Link-Tree -SrcDir $Src -DstDir $Dst
