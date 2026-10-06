# Share's version for a build, by scripts/version.sh's rules: VERSION for the commit tagged with
# it, else what the build was made from, e.g. 0.1.0-dev+abc1234. Dot-source this file, then call
# Get-ShareVersion with the repository's root.
function Get-ShareVersion([string]$Root) {
    $v = (Get-Content -Raw "$Root\VERSION").Trim()
    if ($v -notmatch '^(0|[1-9][0-9]*)\.([0-9]|[1-9][0-9])\.([0-9]|[1-9][0-9])(-[0-9A-Za-z.-]+)?$') {
        throw "VERSION: '$v' isn't a version like 0.1.0 or 0.2.0-rc1, with minor and patch below 100"
    }
    # A build of anything else is a pre-release: 0.1.0-dev, or 0.2.0-rc1.dev for a pre-release.
    $dev = if ($v.Contains('-')) { "$v.dev" } else { "$v-dev" }
    try { $commit = git -C $Root rev-parse --short=7 HEAD 2>$null } catch { $commit = $null }
    if ($LASTEXITCODE -ne 0 -or -not $commit) { return $dev } # not from a git checkout
    $tag = git -C $Root tag --points-at HEAD 'v*' | Select-Object -First 1
    if ($tag -and $tag -ne "v$v") { throw "this commit is tagged $tag, but VERSION says $v" }
    git -C $Root diff --quiet HEAD --
    $dirty = if ($LASTEXITCODE -ne 0) { '.dirty' } else { '' }
    if ($tag -and -not $dirty) { return $v }
    return "$dev+$commit$dirty"
}
