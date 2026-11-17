### Added

- [sdk] `printcompat` now warns when a file for a Core One with INDX does not name the INDX lock feature. Firmware 6.6.2 and newer stops at a warning before such a print. An INDX printer on older firmware now reports the feature as unsupported
- [sdk] `printcompat` now lists the nozzle cleaner check as unverified on every Core One with INDX. The firmware refuses each print until the cleaner is calibrated, and the printer does not report whether it is
- [sdk] `printcompat` now lists the abrasive filament check of firmware 6.8.0 and newer as unverified in one case. The file names a filament type that is no firmware preset, and the nozzle is not reported as hardened. Such a filament may be one a person defined on the printer as abrasive, and the printer does not report that setting
