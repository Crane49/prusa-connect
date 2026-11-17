# Contributing

Bug reports and pull requests are welcome. For a larger change, open an issue
first, so we can agree on the approach before you write the code.

## Reporting an issue

Open a new issue and pick the form that fits: a bug report, an idea or a
question. Each form asks for what the team needs first, such as the version and
the steps that show a bug. An issue is public, so leave out tokens, passwords,
setup codes, IP addresses, host names and printer serial numbers. A
vulnerability goes to the address in [SECURITY.md](SECURITY.md), never into an
issue.

## The contributor licence agreement

Prusa Research can merge a pull request only when every author of its commits
has signed the [Prusa Contributor License Agreement](CLA.md). You keep the
rights to your work. The agreement gives Prusa a licence to use it and to
distribute it, also under other licences than this repository's.

You sign once, in the pull request itself:

1. Open the pull request. The CLA check comments on it and links the agreement.
2. Read [CLA.md](CLA.md).
3. Post this comment, exactly as written:

   ```
   I have read the CLA Document and I hereby sign the CLA
   ```

The check records your GitHub account and the pull request on the
`cla-signatures` branch of this repository, then passes. Your later pull
requests pass without a new comment. When the check has not run again, comment
`recheck`.

Commit with an email address that GitHub links to your account. The check
reads the authors of the commits, and it cannot match a commit whose address
belongs to no account.

If you contribute on behalf of your employer, make sure your employer allows
it, as section 4 of the agreement says.

## Before you open a pull request

- Run `make test` and `make lint`. `make` with no target lists the other
  checks.
- Run `make secrets-check`. It scans the files git tracks or would add for
  a credential, such as an API token or a private key, and CI runs it on every
  pull request. A test value it reports as a secret goes into `.gitleaks.toml`
  by its exact value.
- Keep one change in one pull request.
- Do not report a vulnerability in a pull request or an issue.
  [SECURITY.md](SECURITY.md) says where to send it.
