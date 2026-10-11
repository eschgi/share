# Asks the bucket of config.json's "s3" setting the CORS questions `share check` asks: whether
# Share's pages on public_url and home_url may send a piece (PUT) and continue a download (GET
# with Range and If-Range). For any S3 service; it needs no keys. Shows the bucket's answers
# and why one is refused.
#
#   scripts\check-cors.ps1                                  config.json in this folder
#   scripts\check-cors.ps1 -Config C:\share\config.json
#   scripts\check-cors.ps1 -Origin http://192.168.1.20:8080 an address more to ask about
param(
    [string]$Config = ".\config.json",
    [string[]]$Origin = @()
)

Add-Type -AssemblyName System.Net.Http
$cfg = Get-Content -Raw $Config | ConvertFrom-Json
if (-not $cfg.s3) { Write-Host "No ""s3"" setting in $Config" -ForegroundColor Red; exit 1 }

$origins = @($cfg.public_url)
if ($cfg.home_url) { $origins += $cfg.home_url }
$origins = @($origins + $Origin | ForEach-Object { ([Uri]$_).GetLeftPart([UriPartial]::Authority) } | Select-Object -Unique)

# The object Share asks about, with the bucket in the path; it doesn't have to exist.
$prefix = "$($cfg.s3.prefix)".Trim("/")
if ($prefix) { $prefix += "/" }
$url = "$($cfg.s3.endpoint.TrimEnd('/'))/$($cfg.s3.bucket)/${prefix}files/cors"
Write-Host "Asking $url`n"

$client = New-Object System.Net.Http.HttpClient
function Get-Header($res, [string]$name) {
    $vals = $null
    if ($res.Headers.TryGetValues($name, [ref]$vals)) { return ($vals -join ",") }
    return ""
}
function Test-ListHas([string]$list, [string]$item) {
    foreach ($x in $list.Split(",")) { if ($x.Trim() -ieq $item) { return $true } }
    return $false
}

$questions = @(
    @{ Name = "upload (PUT)"; Method = "PUT"; Headers = @() },
    @{ Name = "continue a download (GET)"; Method = "GET"; Headers = @("range", "if-range") }
)

$failed = 0
foreach ($o in $origins) {
    Write-Host "== $o" -ForegroundColor Cyan
    foreach ($q in $questions) {
        $req = New-Object System.Net.Http.HttpRequestMessage([System.Net.Http.HttpMethod]::Options, $url)
        [void]$req.Headers.TryAddWithoutValidation("Origin", $o)
        [void]$req.Headers.TryAddWithoutValidation("Access-Control-Request-Method", $q.Method)
        if ($q.Headers.Count) {
            [void]$req.Headers.TryAddWithoutValidation("Access-Control-Request-Headers", ($q.Headers -join ","))
        }
        try { $res = $client.SendAsync($req).GetAwaiter().GetResult() }
        catch { Write-Host "  $($q.Name): no answer: $($_.Exception.Message)" -ForegroundColor Red; $failed++; continue }

        $status = [int]$res.StatusCode
        $allowO = Get-Header $res "Access-Control-Allow-Origin"
        $allowM = Get-Header $res "Access-Control-Allow-Methods"
        $allowH = Get-Header $res "Access-Control-Allow-Headers"
        $why = @()
        if ([int][Math]::Floor($status / 100) -ne 2) { $why += "status is $status, not 2xx" }
        if ($allowO -ne $o -and $allowO -ne "*") { $why += "Allow-Origin is '$allowO', not '$o'" }
        if (-not (Test-ListHas $allowM $q.Method)) { $why += "Allow-Methods '$allowM' lacks $($q.Method)" }
        foreach ($h in $q.Headers) {
            if (-not ((Test-ListHas $allowH $h) -or (Test-ListHas $allowH "*"))) { $why += "Allow-Headers '$allowH' lacks $h" }
        }

        if ($why.Count) {
            Write-Host "  $($q.Name): REFUSED" -ForegroundColor Red
            $why | ForEach-Object { Write-Host "    - $_" }
            $failed++
        } else {
            Write-Host "  $($q.Name): ok" -ForegroundColor Green
        }
        Write-Host "    status $status | origin '$allowO' | methods '$allowM' | headers '$allowH'" -ForegroundColor DarkGray
    }
}

Write-Host ""
if ($failed) { Write-Host "$failed check(s) refused: Share warns about the bucket's CORS rules." -ForegroundColor Red; exit 1 }
Write-Host "All checks pass: Share's CORS warning is gone." -ForegroundColor Green
