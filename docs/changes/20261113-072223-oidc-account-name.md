### Fixed

- A person signed in through Synology SSO was named by their email address. The name is now read from the `username` claim, which Synology SSO sends in place of `preferred_username`.
- Settings > Account offered a password change to a person signed in at an OpenID Connect provider, who has no password on this server. The page now shows their name and hides the password change.
