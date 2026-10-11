# Sets the CORS rules Share's pages need on the Backblaze B2 bucket of config.json's "s3"
# setting, for public_url and home_url, through B2's own API. It replaces the bucket's CORS
# rules, and prints the old ones first. It asks for a key that may change the bucket
# (listBuckets and writeBuckets, e.g. the master key) and keeps nothing.
# scripts\check-cors.ps1 checks the result.
#
#   scripts\b2-cors.ps1                                  config.json in this folder
#   scripts\b2-cors.ps1 -Config C:\share\config.json
#   scripts\b2-cors.ps1 -Origin http://192.168.1.20:8080 an address more, e.g. the LAN one
param(
    [string]$Config = ".\config.json",
    [string[]]$Origin = @()
)

Add-Type -AssemblyName System.Net.Http
$client = New-Object System.Net.Http.HttpClient

# Invoke-B2 calls B2's API and returns the answer, or throws with B2's own message. It sends
# the requests itself: PowerShell 7's Invoke-RestMethod refuses B2's token in Authorization.
function Invoke-B2([string]$url, [string]$auth, $body) {
    $method = if ($body) { [System.Net.Http.HttpMethod]::Post } else { [System.Net.Http.HttpMethod]::Get }
    $req = New-Object System.Net.Http.HttpRequestMessage($method, $url)
    [void]$req.Headers.TryAddWithoutValidation("Authorization", $auth)
    if ($body) {
        $json = $body | ConvertTo-Json -Depth 6
        $req.Content = New-Object System.Net.Http.StringContent($json, [Text.Encoding]::UTF8, "application/json")
    }
    $res = $client.SendAsync($req).GetAwaiter().GetResult()
    $text = $res.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    if (-not $res.IsSuccessStatusCode) { throw "$([int]$res.StatusCode) from $($url.Split('/')[-1]): $text" }
    return $text | ConvertFrom-Json
}

$cfg = Get-Content -Raw $Config | ConvertFrom-Json
if (-not $cfg.s3) { Write-Host "No ""s3"" setting in $Config" -ForegroundColor Red; exit 1 }
$origins = @($cfg.public_url)
if ($cfg.home_url) { $origins += $cfg.home_url }
$origins = @($origins + $Origin | ForEach-Object { ([Uri]$_).GetLeftPart([UriPartial]::Authority) } | Select-Object -Unique)

Write-Host "Bucket:  $($cfg.s3.bucket)"
Write-Host "Origins: $($origins -join ', ')`n"
Write-Host "Use a key that may change the bucket's settings (e.g. the master key), not Share's key."
$keyId = Read-Host "keyID"
$secure = Read-Host "applicationKey" -AsSecureString
$appKey = [Runtime.InteropServices.Marshal]::PtrToStringBSTR([Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))

try {
    Write-Host "`n1. Signing in..."
    $basic = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes("${keyId}:${appKey}"))
    $auth = Invoke-B2 "https://api.backblazeb2.com/b2api/v2/b2_authorize_account" "Basic $basic" $null

    Write-Host "2. Finding the bucket..."
    $list = Invoke-B2 "$($auth.apiUrl)/b2api/v2/b2_list_buckets" $auth.authorizationToken @{ accountId = $auth.accountId; bucketName = $cfg.s3.bucket }
    if (-not $list.buckets) { throw "bucket $($cfg.s3.bucket) not found for this key" }
    $bucket = $list.buckets[0]
    if ($bucket.corsRules) {
        Write-Host "   Rules now (they will be replaced):"
        $bucket.corsRules | ConvertTo-Json -Depth 5 | Write-Host
    } else {
        Write-Host "   The bucket has no CORS rules yet."
    }

    Write-Host "3. Setting the rules..."
    $rules = @(@{
        corsRuleName      = "share-pages" # B2 wants at least 6 characters
        allowedOrigins    = $origins
        allowedOperations = @("s3_get", "s3_head", "s3_put")
        allowedHeaders    = @("*")
        exposeHeaders     = @("ETag", "Content-Length", "Content-Range")
        maxAgeSeconds     = 3600
    })
    $done = Invoke-B2 "$($auth.apiUrl)/b2api/v2/b2_update_bucket" $auth.authorizationToken @{ accountId = $auth.accountId; bucketId = $bucket.bucketId; corsRules = $rules }

    Write-Host "`nRules set:" -ForegroundColor Green
    $done.corsRules | ConvertTo-Json -Depth 5 | Write-Host
} catch {
    Write-Host "Failed: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
