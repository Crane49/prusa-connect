---
title: "ADR-20261125-jqp: Prusa Connect runs as a Windows service that an installer owns"
description: "On Windows the server runs under the service control manager, writes its log to files, and leaves the replacement of its executable to the installer that put it there."
date: 2026-11-25
status: proposed
supersedes: []
amends: []
tags:
  - adr
---
# ADR-20261125-jqp: Prusa Connect runs as a Windows service that an installer owns

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

On Linux the server runs as a systemd service. On Windows it runs in a
terminal window, and it stops when the window closes or the person signs out.
A server that printers depend on must start at boot and come back after a
failure. On Windows that is the job of the service control manager, the part
of Windows that starts, stops and watches services. This record says how the
server runs under it, and who replaces the executable of such a server.

## rules

1. The same binary runs in a terminal and as a service. The process entry asks
   Windows which of the two started it, and no flag selects it.
2. A service runs the core only. Its command line is the one it was installed
   with, and a stop or shutdown of the service cancels the core's context, as
   Ctrl+C does in a terminal.
3. The service is named `PrusaConnect`, and is told that a stop may take 30
   seconds, longer than the default `core.shutdown_timeout` of 15 seconds.
4. A service is installed with `-workdir`, the folder it runs in. Relative paths
   in the configuration and `-config` itself resolve from that folder, as they
   do from `WorkingDirectory` under systemd.
5. A service writes its log, and the setup code of the first run, to
   `logs/prusa-connect.log` under that folder. The file is moved aside at
   10 MB, and five older files are kept. Standard error, where Go writes a
   panic, goes to `logs/stderr.log`.
6. A restart under a service ends the process with exit code 3, and does not
   start a successor process. The service control manager loses track of a
   process that starts its successor, and would report the service as
   stopped.
7. A service is installed with a recovery action that starts it again after a
   failure, with that action also applied to a non-zero exit code. Exit code 3
   is how rule 6 reaches it.
8. A stop somebody asked for reports success to the service control manager
   whatever the core returned, so the recovery action never starts a service
   a person stopped.
9. An installer that puts the executable on disk also puts a file named
   `managed-by-installer` beside it. With that file present, the server
   prepares an upgrade but does not replace its executable, and the Updates
   page says the installer owns it. A new release is installed by running its
   installer.

## enforcement

| rule | gate |
|---|---|
| 2, 3, 8 | `internal/kernel/winservice/service_windows_test.go`, on the Windows CI job |
| 5 | `internal/kernel/winservice/logfile_test.go` |
| 9 | `internal/app/maintenanceinstaller_test.go` |

## rejected

| option | why not |
|---|---|
| a wrapper such as NSSM or WinSW that runs the terminal program as a service | the restart in rule 6 and the upgrade in rule 9 happen inside the server, and a wrapper cannot see them |
| the server replaces its own executable under Program Files | the installer would then report a version that is not on disk, and a repair or an uninstall would act on the wrong file |
| the Windows event log for every log line | one event per line would fill the log that Windows keeps for every program |
| a flag that selects service mode | a service started without it would run as a terminal program that nobody can stop |

## open items

- the installer: a WiX package that installs the service with rules 4, 7 and
  9, the firewall rules and the data folder under `ProgramData`
- a start failure before rule 5 opens the log, such as a typo in a flag, is
  seen only as the exit code in `sc query PrusaConnect`
