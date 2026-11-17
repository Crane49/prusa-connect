// Package local is the credential store a deployment with no identity provider
// logs in against.
//
// It satisfies sdk/authapi.IdentityProvider, so the host reaches it through the
// published contract and a third-party provider replaces it without the login
// path changing. It is the host's own store and not an adapter under adapters/,
// because authapi.ProviderFactory carries no host services, and a provider
// handed the account rows would be a plugin holding the passwords.
//
// Three rules here are security rules and not conveniences:
//
//   - an unknown username costs the same argon2id pass as a wrong password, so
//     a response time does not enumerate accounts
//   - the price of a guess bounds guessing, not a count of attempts. The
//     password is checked before the failure record is read, so a correct one
//     is always admitted. A count keyed by the account id cannot tell the owner
//     from whoever is guessing at them
//   - the number of argon2id passes running at once is bounded, because each
//     one holds a large block of memory and the caller has no credential
package local
