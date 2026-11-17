// Package extension is the registration rule of every extension point, and
// the descriptor of every registered entry.
//
// A registry gathers declarations at package initialisation and the host
// drains it once. After that a registration is an error. A qualified name may
// be registered once, and a second registration is refused.
//
// A name is qualified by a namespace derived from the module of the package
// that registered it. An author cannot choose it. Only the main module of a
// build has the core namespace.
//
// Doc says which descriptor fields are mandatory.
package extension
