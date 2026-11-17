# Preflight for a printer plugged straight into this machine's ethernet port.
#
# A direct cable has no router on it, which removes three things a printer
# normally gets for free: an address, a route, and a host that answers. Each
# failure looks the same from the printer's side, which is nothing happening,
# so this checks them separately and says which one is wrong.
#
#   powershell -ExecutionPolicy Bypass -File scripts/bench-preflight.ps1
#
# Add -Fix to create the firewall rule, which needs an elevated shell.
#
# Every check prints OK with what it observed, FAIL with what to do, or WARN
# when this shell cannot observe the thing at all. WARN is never a pass. The
# desk-side page is docs/src/content/docs/bench-preflight.md.

[CmdletBinding()]
param(
    [int]$Port = 8080,
    [string]$AdapterName = 'Ethernet',
    [switch]$Fix
)

$ErrorActionPreference = 'Stop'

# The port shipping firmware dials when nobody changes it, and the port the
# container publishes for the same listener. Checked beside $Port because a
# printer that was never reconfigured arrives here.
$firmwareDefaultPort = 80

# Processes that hold a port on behalf of a container rather than on behalf of
# a server. Docker Desktop on Windows publishes through these, so seeing one
# own the port is direct evidence that the container answers and the core does
# not.
$containerProxies = @('com.docker.backend', 'vpnkit', 'wslrelay', 'dockerd', 'docker-proxy')

$failed = @()
$unproven = @()

function Report {
    param(
        [string]$Check,
        [ValidateSet('ok', 'fail', 'warn')][string]$State,
        [string]$Detail
    )

    $mark = switch ($State) {
        'ok'   { '  OK  ' }
        'fail' { ' FAIL ' }
        'warn' { ' WARN ' }
    }

    Write-Host ("[{0}] {1}" -f $mark, $Check)
    if ($Detail) { Write-Host ("        {0}" -f $Detail) }

    if ($State -eq 'fail') { $script:failed += $Check }
    if ($State -eq 'warn') { $script:unproven += $Check }
}

function Test-Elevated {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()

    return (New-Object Security.Principal.WindowsPrincipal($identity)).IsInRole(
        [Security.Principal.WindowsBuiltInRole]::Administrator)
}

# A firewall rule states its ports as Any, a number, a range, or a list of
# those. A keyword such as RPC names a port this cannot resolve, so it never
# counts as a match.
function Test-RuleCoversPort {
    param($LocalPort, [int]$Wanted)

    foreach ($entry in @($LocalPort)) {
        $text = [string]$entry

        if ($text -eq 'Any') { return $true }

        if ($text -match '^(\d+)-(\d+)$') {
            if ($Wanted -ge [int]$Matches[1] -and $Wanted -le [int]$Matches[2]) { return $true }

            continue
        }

        if ($text -match '^\d+$' -and [int]$text -eq $Wanted) { return $true }
    }

    return $false
}

# The process names behind a set of listening sockets, with their pids, so the
# line reads as evidence rather than as a claim.
function Get-ListenerOwners {
    param($Listeners)

    $owners = @{}

    foreach ($listener in $Listeners) {
        $process = Get-Process -Id $listener.OwningProcess -ErrorAction SilentlyContinue
        $name = if ($process) { $process.ProcessName } else { 'unknown' }
        $owners[("{0} (pid {1})" -f $name, $listener.OwningProcess)] = $name
    }

    return $owners
}

# Both addresses in the same subnet, read through this machine's prefix length.
function Test-SameSubnet {
    param([string]$Left, [string]$Right, [int]$PrefixLength)

    $leftBytes = [byte[]]([Net.IPAddress]::Parse($Left).GetAddressBytes()[3..0])
    $rightBytes = [byte[]]([Net.IPAddress]::Parse($Right).GetAddressBytes()[3..0])
    $leftBits = [BitConverter]::ToUInt32($leftBytes, 0)
    $rightBits = [BitConverter]::ToUInt32($rightBytes, 0)
    $mask = if ($PrefixLength -le 0) { 0L } else { (0xFFFFFFFFL -shl (32 - $PrefixLength)) -band 0xFFFFFFFFL }

    return ([int64]$leftBits -band $mask) -eq ([int64]$rightBits -band $mask)
}

# GET /api/v1/health on the port, which only the core serves. The shipping
# server has no path under /api/v1 and answers 404 there, so the reply
# separates the two servers without reading a log.
function Invoke-HealthProbe {
    param([string]$Uri)

    $result = @{ Reached = $false; Status = 0; Body = ''; Error = '' }

    try {
        $response = Invoke-WebRequest -Uri $Uri -UseBasicParsing -TimeoutSec 5
        $result.Reached = $true
        $result.Status = [int]$response.StatusCode
        $result.Body = $response.Content
    } catch [Net.WebException] {
        if ($_.Exception.Response) {
            $result.Reached = $true
            $result.Status = [int]$_.Exception.Response.StatusCode
        }
        $result.Error = $_.Exception.Message
    } catch {
        $result.Error = $_.Exception.Message
    }

    return $result
}

# POST /p/register, the first request a printer makes. It is plain HTTP and
# carries no credential, so this machine can make it and read what a printer
# would read.
#
# The Code header is the whole reading, and the status is not. The frontend is
# mounted at / and answers any path no route claims, so a core with no
# connector mounted answers 200 with a page. A printer meeting that gets a
# success it cannot use.
#
# The fingerprint is fixed. The connector returns the code it already issued
# for a fingerprint it has seen, so repeated runs leave one registration behind
# rather than one per run.
function Invoke-RegistrationProbe {
    param([string]$Uri)

    $result = @{ Reached = $false; Status = 0; Code = ''; Error = '' }

    $body = @{
        sn           = 'BENCH-PREFLIGHT'
        fingerprint  = 'BENCHPREFLIGHT0000000000000000AA'
        printer_type = '7.1.0'
        firmware     = '0.0.0'
    } | ConvertTo-Json -Compress

    try {
        $response = Invoke-WebRequest -Uri $Uri -Method Post -Body $body `
            -ContentType 'application/json' -UseBasicParsing -TimeoutSec 5
        $result.Reached = $true
        $result.Status = [int]$response.StatusCode
        $result.Code = [string]$response.Headers['Code']
    } catch [Net.WebException] {
        if ($_.Exception.Response) {
            $result.Reached = $true
            $result.Status = [int]$_.Exception.Response.StatusCode
        }
        $result.Error = $_.Exception.Message
    } catch {
        $result.Error = $_.Exception.Message
    }

    return $result
}

Write-Host ''
Write-Host '  prusa-connect: bench preflight'
Write-Host ''

$elevated = Test-Elevated

# 1. The cable. Everything else is meaningless until the link is up, and a
#    disconnected adapter is the one failure that is not worth diagnosing
#    further.
$adapter = Get-NetAdapter -Name $AdapterName -ErrorAction SilentlyContinue

if (-not $adapter) {
    $present = (Get-NetAdapter -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name) -join ', '
    Report 'the ethernet adapter exists' 'fail' `
        ("no adapter is named '$AdapterName'. This machine has: $present. Pass -AdapterName with the one the cable is in.")
} elseif ($adapter.Status -eq 'Up') {
    Report 'the ethernet link is up' 'ok' `
        ("$AdapterName is Up at $($adapter.LinkSpeed), MAC $($adapter.MacAddress)")
} else {
    Report 'the ethernet link is up' 'fail' `
        ("$AdapterName reports $($adapter.Status) at $($adapter.LinkSpeed). Seat the cable at both ends and power the printer on.")
}

# 2. An address on that adapter. With no DHCP server on the cable, Windows
#    falls back to 169.254.x.x after roughly a minute. PrefixOrigin says which
#    of the three ways the address arrived, so the line reports what happened
#    rather than guessing from the number.
$address = $null

if (-not $adapter) {
    Report 'the adapter has an IPv4 address' 'warn' 'there is no such adapter, so no address could be read'
} elseif ($adapter.Status -ne 'Up') {
    Report 'the adapter has an IPv4 address' 'warn' 'the link is down, so any address on it is stale'
} else {
    $address = Get-NetIPAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object { $_.IPAddress -notlike '127.*' } |
        Select-Object -First 1

    if (-not $address) {
        $dhcp = Get-NetIPInterface -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue
        Report 'the adapter has an IPv4 address' 'fail' `
            ("no IPv4 address on $AdapterName, and DHCP on it is $($dhcp.Dhcp). Wait a minute for the self-assigned address, or set a static address here and on the printer.")
    } else {
        $origin = switch ($address.PrefixOrigin) {
            'Dhcp'      { 'handed out by a DHCP server, so something on this cable is serving DHCP' }
            'Manual'    { 'set manually on this adapter' }
            'WellKnown' { 'self-assigned, because no DHCP server answered on this cable' }
            default     { "origin $($address.PrefixOrigin)" }
        }

        Report 'the adapter has an IPv4 address' 'ok' `
            ("$($address.IPAddress)/$($address.PrefixLength) on $AdapterName, $origin")
    }
}

# 3. The printer at the other end. A neighbour entry is the only proof this
#    machine has that the printer is powered, addressed, and on the same
#    subnet. Without it the two ends can be perfectly configured and still
#    never meet, which is what a missing DHCP server does to a direct cable.
if (-not $address) {
    Report 'the printer answers on the cable' 'warn' 'this machine has no address on that adapter, so nothing can be reached through it'
} else {
    $neighbours = Get-NetNeighbor -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object {
            $_.State -in @('Reachable', 'Stale', 'Delay', 'Probe') -and
            $_.IPAddress -notmatch '^(22[4-9]|23[0-9])\.' -and
            $_.IPAddress -ne '255.255.255.255'
        }

    if (-not $neighbours) {
        Report 'the printer answers on the cable' 'fail' `
            ("nothing has spoken on $AdapterName. Power the printer on, give it a minute, then re-run. If you know its address, run: ping <printer-address>")
    } else {
        $reachable = @($neighbours | Where-Object { Test-SameSubnet $_.IPAddress $address.IPAddress $address.PrefixLength })

        if ($reachable.Count -eq 0) {
            $seen = ($neighbours | Select-Object -ExpandProperty IPAddress) -join ', '
            Report 'the printer answers on the cable' 'fail' `
                ("the other end is at $seen, which is not inside $($address.IPAddress)/$($address.PrefixLength). Give both ends a static address in one subnet, or let both self-assign in 169.254.")
        } else {
            $seen = ($reachable | ForEach-Object { "$($_.IPAddress) ($($_.State))" }) -join ', '
            Report 'the printer answers on the cable' 'ok' ("reached $seen on $AdapterName")
        }
    }
}

# 4. The firewall. This is the failure that wastes the most time, because the
#    server is running, the printer is addressed, and the connection is
#    dropped before anything logs it. A direct cable usually lands on the
#    Public profile, which blocks inbound by default.
#
#    Reading the port filters needs administrator rights. Without them the
#    list comes back short and a missing rule cannot be told from a rule that
#    was not readable, so this reports WARN instead of FAIL.
$ruleName = "prusa-connect bench (TCP $Port)"
$activeProfile = $null

if ($adapter) {
    $connection = Get-NetConnectionProfile -InterfaceIndex $adapter.ifIndex -ErrorAction SilentlyContinue
    if ($connection) {
        $activeProfile = switch ($connection.NetworkCategory) {
            'DomainAuthenticated' { 'Domain' }
            'Private'             { 'Private' }
            default               { 'Public' }
        }
    }
}

$profilesToCheck = if ($activeProfile) { @($activeProfile) } else { @('Domain', 'Private', 'Public') }
$enabledProfiles = @(Get-NetFirewallProfile -ErrorAction SilentlyContinue |
    Where-Object { $_.Enabled -and $_.Name -in $profilesToCheck })

$where = if ($activeProfile) {
    "the $activeProfile profile, which is the one $AdapterName is on"
} else {
    "all three profiles, because $AdapterName has no network profile yet"
}

if ($enabledProfiles.Count -eq 0) {
    Report "the firewall admits TCP $Port" 'ok' ("the firewall is off on $where")
} else {
    $ruleError = $null
    $filterError = $null

    $allowed = @{}
    foreach ($rule in Get-NetFirewallRule -Direction Inbound -Enabled True -Action Allow `
            -ErrorAction SilentlyContinue -ErrorVariable ruleError) {
        $allowed[$rule.Name] = $rule
    }

    $filters = @(Get-NetFirewallPortFilter -ErrorAction SilentlyContinue -ErrorVariable filterError)

    # Windows ships many inbound rules whose port is Any and whose program or
    # service is one named binary: Wi-Fi Direct printing, Remote Assistance and
    # their siblings all match TCP 8080 on port alone. A store app adds another
    # shape, with Program set to Any and the package named in the rule's Owner
    # instead. None of them admits the core, so a rule counts only when it is
    # open to every program. Measured on this machine: 289 inbound allow rules,
    # of which none is.
    $programs = @{}
    $packages = @{}
    foreach ($filter in Get-NetFirewallApplicationFilter -ErrorAction SilentlyContinue) {
        $programs[$filter.InstanceID] = [string]$filter.Program
        $packages[$filter.InstanceID] = [string]$filter.Package
    }

    $services = @{}
    foreach ($filter in Get-NetFirewallServiceFilter -ErrorAction SilentlyContinue) {
        $services[$filter.InstanceID] = [string]$filter.Service
    }

    $matching = @()
    $narrowed = @()

    foreach ($filter in $filters) {
        if (-not $allowed.ContainsKey($filter.InstanceID)) { continue }
        if ($filter.Protocol -notin @('TCP', 'Any')) { continue }
        if (-not (Test-RuleCoversPort $filter.LocalPort $Port)) { continue }

        $rule = $allowed[$filter.InstanceID]
        $scope = [string]$rule.Profile

        if ($scope -ne 'Any' -and -not ($enabledProfiles | Where-Object { $scope -match $_.Name })) { continue }

        $program = $programs[$filter.InstanceID]
        $service = $services[$filter.InstanceID]
        $package = $packages[$filter.InstanceID]

        if (($program -and $program -ne 'Any') -or ($service -and $service -ne 'Any') -or
            $package -or $rule.Owner) {
            $narrowed += $rule.DisplayName

            continue
        }

        $matching += ("'{0}' on {1}" -f $rule.DisplayName, $scope)
    }

    $blocked = ($enabledProfiles | Select-Object -ExpandProperty Name) -join ', '
    $incomplete = (@($ruleError).Count + @($filterError).Count) -gt 0

    $alsoSeen = ''
    if ($narrowed.Count -gt 0) {
        $alsoSeen = (" {0} rule(s) reach TCP $Port for one program only, such as '{1}', and none of them admits the core." `
            -f $narrowed.Count, ($narrowed | Select-Object -First 1))
    }

    if ($matching.Count -gt 0) {
        Report "the firewall admits TCP $Port" 'ok' (($matching | Select-Object -First 3) -join '; ')
    } elseif ($Fix -and $elevated) {
        try {
            New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Action Allow `
                -Protocol TCP -LocalPort $Port -Profile Any -ErrorAction Stop | Out-Null
            Report "the firewall admits TCP $Port" 'ok' "created the rule '$ruleName' on every profile"
        } catch {
            Report "the firewall admits TCP $Port" 'fail' `
                ("the rule could not be created: $($_.Exception.Message)")
        }
    } elseif ($Fix) {
        Report "the firewall admits TCP $Port" 'fail' `
            ("no rule admits TCP $Port and the firewall is on for: $blocked.$alsoSeen -Fix needs an elevated shell, and this one is not elevated.")
    } elseif ($incomplete) {
        Report "the firewall admits TCP $Port" 'warn' `
            ("no rule admits TCP $Port on $where, and the rule list came back short: reading all of it needs administrator rights.$alsoSeen Re-run this in an elevated shell to be sure.")
    } else {
        Report "the firewall admits TCP $Port" 'fail' `
            ("no rule admits TCP $Port and the firewall is on for: $blocked.$alsoSeen Re-run with -Fix in an elevated shell, which creates '$ruleName'.")
    }
}

# 5. Something listening, and which program it is. The owning process is what
#    separates the core from a container: Docker Desktop publishes a port
#    through com.docker.backend, so that name on the port means the container
#    answers whatever the core is doing.
$listeners = @(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue)
$owners = @{}

if ($listeners.Count -eq 0) {
    Report "something is listening on $Port" 'fail' `
        ('nothing is. Build it with: make build, then run: .\bin\server.exe core --config config\config.yaml')
} else {
    $owners = Get-ListenerOwners $listeners
    Report "something is listening on $Port" 'ok' (($owners.Keys | Sort-Object) -join ', ')
}

# 6. Listening where the printer can reach it. A server bound to 127.0.0.1
#    answers every test run from this machine and is invisible on the cable,
#    which is the second most expensive failure.
if ($listeners.Count -eq 0) {
    Report "the listener on $Port is reachable from the cable" 'warn' "nothing is listening on $Port, so there is no bind address to read"
} else {
    $bound = ($listeners | Select-Object -ExpandProperty LocalAddress -Unique) -join ', '
    $wildcard = @($listeners | Where-Object { $_.LocalAddress -in @('0.0.0.0', '::') })

    if ($wildcard.Count -eq 0) {
        Report "the listener on $Port is reachable from the cable" 'fail' `
            ("bound to $bound only. Set core.api.addr to "":$Port"", not ""127.0.0.1:$Port"".")
    } else {
        Report "the listener on $Port is reachable from the cable" 'ok' ("bound on $bound")
    }
}

# 7. Which server holds the port. The docker stack publishes the shipping
#    server on 80 and 8080, so a printer pointed at this machine on a default
#    port reaches that one and not the core being tested. Nothing about that
#    looks wrong: the printer connects, and the developer view stays empty.
$dockerCommand = Get-Command docker -ErrorAction SilentlyContinue
$proxyOwners = @($owners.Values | Where-Object { $_ -in $containerProxies }) | Select-Object -Unique

if (-not $dockerCommand) {
    Report 'the core holds the port, not a container' 'ok' 'docker is not installed here, so no container can publish a port'
} else {
    $publishers = @()
    $dockerFailure = ''

    foreach ($candidate in @($firmwareDefaultPort, $Port) | Select-Object -Unique) {
        # Windows PowerShell wraps a native command's stderr in an error record,
        # which a Stop preference turns into a thrown exception. The exit code
        # is the reading that matters, so the preference is lowered around the
        # call and restored after it.
        $previous = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $output = & docker ps --filter "publish=$candidate" --format '{{.Names}}' 2>&1
        $code = $LASTEXITCODE
        $ErrorActionPreference = $previous

        if ($code -ne 0) {
            $dockerFailure = ($output | Out-String).Trim()

            break
        }

        $names = @($output | Where-Object { $_ })
        if ($names.Count -gt 0) {
            $publishers += ("port {0} is published by {1}" -f $candidate, ($names -join ', '))
        }
    }

    if ($dockerFailure) {
        Report 'the core holds the port, not a container' 'warn' `
            ("the docker daemon did not answer, so nothing here read what containers publish: $dockerFailure")
    } elseif ($publishers.Count -gt 0) {
        Report 'the core holds the port, not a container' 'fail' `
            (($publishers -join '; ') + ". Stop it with: docker compose -f docker-compose.local.yml stop prusa-connect")
    } elseif ($proxyOwners.Count -gt 0) {
        Report 'the core holds the port, not a container' 'fail' `
            (("$Port is held by " + ($proxyOwners -join ', ')) + ", which publishes a container port. Find it with: docker ps --filter publish=$Port")
    } else {
        Report 'the core holds the port, not a container' 'ok' `
            ("no container publishes $firmwareDefaultPort or $Port, and no container proxy holds the port")
    }
}

# 8. The reply itself, which settles what the checks above only argue. Only
#    the core serves /api/v1/health; the shipping server has no path under
#    /api/v1 and answers 404 there.
if ($listeners.Count -eq 0) {
    Report "the core answers on $Port" 'warn' "nothing is listening on $Port, so there was nothing to ask"
} else {
    $probe = Invoke-HealthProbe ("http://127.0.0.1:{0}/api/v1/health" -f $Port)

    if ($probe.Reached -and $probe.Status -eq 200 -and $probe.Body -match '"status"\s*:\s*"ok"') {
        $build = if ($probe.Body -match '"version"\s*:\s*"([^"]*)"') { $Matches[1] } else { 'unstamped' }
        Report "the core answers on $Port" 'ok' ("GET /api/v1/health returned 200, build '$build'")
    } elseif ($probe.Reached) {
        Report "the core answers on $Port" 'fail' `
            ("GET /api/v1/health returned $($probe.Status). The core returns 200 there and the shipping server returns 404, so this is not the core. Stop whatever holds the port, or move the core to a port nothing else uses.")
    } else {
        Report "the core answers on $Port" 'warn' `
            ("http://127.0.0.1:$Port did not answer: $($probe.Error). If the connector's listener.tls is set, this port speaks TLS and this probe cannot read it.")
    }
}

# 9. The printer's own first request. Everything above proves a person can
#    reach the core; this proves a printer can. They are different surfaces:
#    the API answers /api/v1 and the connector answers /p/register, and a core
#    with no connector mounted serves the first and not the second.
if ($listeners.Count -eq 0) {
    Report 'the connector answers the printer wire' 'warn' `
        "nothing is listening on $Port, so no registration could be made"
} else {
    $registration = Invoke-RegistrationProbe ("http://127.0.0.1:{0}/p/register" -f $Port)

    if ($registration.Code) {
        Report 'the connector answers the printer wire' 'ok' `
            ("POST /p/register returned $($registration.Status) and Code $($registration.Code)")
    } elseif ($registration.Reached) {
        Report 'the connector answers the printer wire' 'fail' `
            ("POST /p/register returned $($registration.Status) and no Code header. Read the Code and not the status: the frontend is mounted at / and answers any path no route claims. Check that core.connectors names a connector, or leave the key out to mount the shipping xBuddy dialect.")
    } else {
        Report 'the connector answers the printer wire' 'warn' `
            ("http://127.0.0.1:$Port/p/register did not answer: $($registration.Error). If the connector's listener.tls is set, this port speaks TLS and this probe cannot read it.")
    }
}

Write-Host ''

if ($failed.Count -gt 0) {
    Write-Host ("  {0} check(s) failed: {1}" -f $failed.Count, ($failed -join '; '))
}

if ($unproven.Count -gt 0) {
    Write-Host ("  {0} check(s) could not be run here: {1}" -f $unproven.Count, ($unproven -join '; '))
}

if ($failed.Count -gt 0 -or $unproven.Count -gt 0) {
    Write-Host ''
    Write-Host '  docs/src/content/docs/bench-preflight.md says what to do about each one.'
    Write-Host ''

    exit 1
}

# Everything passed, so the last useful thing is the address to type into the
# printer. The firmware dials /p/register and /p/ws at whatever host it is
# given, so this is the whole configuration on that side.
Write-Host '  Point the printer at this machine:'
Write-Host ''
Write-Host ("      hostname = {0}" -f $address.IPAddress)
Write-Host ("      port     = {0}" -f $Port)
Write-Host ''
Write-Host '  Then watch it arrive:'
Write-Host ''
Write-Host ("      http://127.0.0.1:{0}/dev" -f $Port)
Write-Host ''
