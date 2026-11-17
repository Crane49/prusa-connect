### Fixed

- Under the systemd unit from `prusa-connect.service`, the server now announces itself over mDNS on its network addresses. The first-run page and the log banner now offer those addresses too
- A typo in the configuration file now stops the server with the key and its line alone. The message no longer starts with "recover interrupted backup restore"
