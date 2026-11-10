# Installs Elsewhere (the terminal player) and Melisma (`dhunn`, the engine that chooses what
# plays next) on Windows, then sets Melisma up: who's listening, and your music analyzed.
#
#   irm https://raw.githubusercontent.com/KrishBnsl/elsewhere/main/install.ps1 | iex
#
# With options:
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/KrishBnsl/elsewhere/main/install.ps1))) -Music D:\Music
#
# Options: -BinDir <folder> (default: %LOCALAPPDATA%\Programs\Elsewhere), -Music <folder>
# (default: your Music folder), -Name <name>, -NoSetup, -NoModifyPath, -Yes, -Uninstall.
# ELSEWHERE_RELEASE_URL overrides where the download comes from (a local folder works too).
# Chiptune Radio isn't available on Windows.

param(
    [string]$BinDir = (Join-Path $env:LOCALAPPDATA 'Programs\Elsewhere'),
    [string]$Music = [Environment]::GetFolderPath('MyMusic'),
    [string]$Name = '',
    [switch]$NoSetup,
    [switch]$NoModifyPath,
    [switch]$Yes,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'  # Invoke-WebRequest is much faster without it.
$Release = if ($env:ELSEWHERE_RELEASE_URL) { $env:ELSEWHERE_RELEASE_URL } else {
    'https://github.com/KrishBnsl/elsewhere/releases/latest/download'
}
$Asset = 'elsewhere-windows-x86_64.zip'

function Say($text) { Write-Host "▸ $text" -ForegroundColor DarkYellow }
function Note($text) { Write-Host "  $text" }
function Ask($question, $default) {
    if ($Yes -or -not [Environment]::UserInteractive -or [Console]::IsInputRedirected) { return $default }
    $answer = Read-Host "$question"
    if ([string]::IsNullOrWhiteSpace($answer)) { return $default }
    return $answer.Trim()
}

if ($Uninstall) {
    foreach ($program in 'elsewhere.exe', 'dhunn.exe') {
        $path = Join-Path $BinDir $program
        if (Test-Path $path) { Remove-Item $path -Force; Say "removed $path" }
    }
    Note "Your Melisma library and history are still in $env:APPDATA\dhunn,"
    Note "and Elsewhere's likes, playlists and theme in $env:APPDATA\Elsewhere."
    return
}

if (-not [Environment]::Is64BitOperatingSystem) { throw 'Elsewhere needs 64-bit Windows.' }

# ---- 1. The programs ----

$work = Join-Path ([IO.Path]::GetTempPath()) ("elsewhere-install-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $work, $BinDir | Out-Null
try {
    Say 'Downloading Elsewhere and Melisma for Windows'
    $zip = Join-Path $work $Asset
    $sum = "$zip.sha256"
    if (Test-Path $Release) {
        Copy-Item (Join-Path $Release $Asset) $zip
        Copy-Item (Join-Path $Release "$Asset.sha256") $sum -ErrorAction SilentlyContinue
    } else {
        Invoke-WebRequest -UseBasicParsing "$Release/$Asset" -OutFile $zip
        try { Invoke-WebRequest -UseBasicParsing "$Release/$Asset.sha256" -OutFile $sum } catch {}
    }
    if (Test-Path $sum) {
        $want = ((Get-Content $sum -Raw).Trim() -split '\s+')[0].ToLower()
        $got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
        if ($want -ne $got) { throw 'The download is damaged (checksum mismatch); try again.' }
    }
    Expand-Archive -Path $zip -DestinationPath $work -Force
    foreach ($program in 'elsewhere.exe', 'dhunn.exe') {
        $from = Join-Path $work $program
        if (-not (Test-Path $from)) { throw "The download is missing $program." }
        Copy-Item $from (Join-Path $BinDir $program) -Force
    }
} finally {
    Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}
Say "Installed elsewhere and dhunn in $BinDir"

# ---- 2. PATH ----

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries = @($userPath -split ';' | Where-Object { $_ })
if ($entries -notcontains $BinDir) {
    if ($NoModifyPath) {
        Say "Add $BinDir to your PATH to run elsewhere from anywhere"
    } else {
        [Environment]::SetEnvironmentVariable('Path', (($entries + $BinDir) -join ';'), 'User')
        Say "Added $BinDir to your PATH (new terminals pick it up)"
    }
}
if (($env:Path -split ';') -notcontains $BinDir) { $env:Path = "$env:Path;$BinDir" }
$dhunn = Join-Path $BinDir 'dhunn.exe'

# ---- 3. Melisma's setup ----

if (-not $NoSetup) {
    $users = (& $dhunn user list 2>$null) -join "`n"
    if ($users -match '^No users yet') {
        if (-not $Name) { $Name = Ask "Who's listening? (a name for your taste profile) [$env:USERNAME]" $env:USERNAME }
        & $dhunn user add $Name | Out-Null
        Say "Melisma will learn $Name's taste"
    }
    if (Test-Path $Music) {
        $answer = Ask "Let Melisma listen to the music in $Music now? (about a second a song, once) [Y/n]" 'y'
        if ($answer -notmatch '^[nN]') {
            & $dhunn analyze $Music -q
        } else {
            Note "Later: dhunn analyze `"$Music`" (or /analyze inside Elsewhere)"
        }
    } else {
        Note "No folder at $Music. Later: dhunn analyze <your music folder>"
    }
}

# ---- 4. Done ----

Write-Host ''
Say 'Ready. In Windows Terminal, run:  elsewhere'
Note "Type /melisma to let Melisma choose what plays next; /q shows what's coming."
if ($Music -ne [Environment]::GetFolderPath('MyMusic')) { Note "Your music is elsewhere: elsewhere `"$Music`"" }
