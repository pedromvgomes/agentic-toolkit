# Cloud init signs with the user's own key when one is supplied

A **Cloud session** commits as the platform's own identity (`Claude <noreply@anthropic.com>`) and
signs through the platform's signer, so a user's commits there carry neither their name nor a
signature GitHub attributes to them. `agtk cloud init` gives the session the user's own identity
and, when the user supplies one, the user's own signing key. This ADR records how, what was
rejected, and the limit the design cannot remove.

## Decision

**Identity is always the user's.** `AGTK_GH_USER` sets git `user.name` and `AGTK_GH_EMAIL` sets
`user.email`, globally and in each checkout that carries no repo-local identity of its own. Each
variable applies on its own: setting one does not require the other. A repo-local identity is the
user's deliberate choice for that repository and is left alone.

**Signing is optional and uses a key the user generated.** `AGTK_SIGNING_KEY_B64` carries a
base64 OpenSSH private key with no passphrase. The command writes it to
`~/.ssh/agtk_signing_key` at mode 0600 in a 0700 directory, through a temp file, validates it with
`ssh-keygen -y`, derives the `.pub`, and sets `gpg.format ssh`, `user.signingkey` (the `.pub`),
`gpg.ssh.program ssh-keygen` (overriding the platform's signer), and `commit.gpgsign` and
`tag.gpgsign` true. It then proves the setup by signing a temporary blob with no terminal and a
timeout, and prints the key's fingerprint, never the key.

**With no key, no signing setting is touched.** Commits then show as Unverified on GitHub, which
is accepted: an unsigned commit under the user's name is a smaller wrong than a signature that is
not the user's. Nothing a prior run set is cleared.

**The command is inert when nothing is configured.** With none of the three variables set it
changes nothing, prints one line saying so, and exits 0. A session-start hook is expected to run
it in every session of every repository that adopts it, including repositories and users that
never configured it, so "not configured" cannot be an error and cannot have an effect.

**The command exports nothing to the environment and is exempt from the background update
check.** Its effect lives in git configuration and one file, so a later process sees it through
those and not through an inherited variable.

## Considered options

**A token or encryption scheme that hands the container a key it can decrypt.** Rejected: the
container has to be able to decrypt it, so the agent running in the container can too. It adds
machinery without moving the key out of the agent's reach.

**A secret embedded in the open-source binary.** Rejected: the binary and its source are public,
so the secret is public, and it ties every issued token to the binary version that holds the
secret.

**The platform signer's own key.** Rejected: whether that key is per user or shared across users
is unknowable from inside the container, and the user will not let anyone else sign as them. A
signature that may be another party's is worse than none.

## Known limits

- **The agent can read the key.** The environment variable and the 0600 file are both readable by
  the agent, which runs as the same user. Nothing in this design hides the key from the process
  that is meant to be constrained by it.
- **The mitigation is blast radius, not secrecy.** Use a signing-only key, registered on GitHub
  only as a signing key (never as an authentication key), and rotate it. A leaked signing-only key
  lets someone sign commits as the user; registered only as a signing key, it is not an
  authentication credential, and rotating it ends the exposure.
- **A best-effort `unset` of the variable was considered and deliberately left out.** The file
  stays readable, and a child process or a transcript may already have seen the value, so an
  `unset` would look like protection without being any.
- **Unsigned sessions show Unverified.** That is the accepted result of supplying no key.

## Consequences

- A user who wants signed commits in a **Cloud session** supplies a dedicated key and accepts the
  limit above; a user who does not gets the right identity and Unverified commits.
- Running the command twice is safe: identity and signing settings are set to the same values, and
  an existing repo-local identity is never overwritten.
- ADR 0002 holds: the command is deterministic and calls no model.
