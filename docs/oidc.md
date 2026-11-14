# OpenID Connect login

Kallisti can offer one operator-configured OpenID Connect (OIDC) provider
alongside its existing password, guest, and Fediverse login methods. The
feature is off when no OIDC settings are supplied. It is intended for providers
such as Pocket ID, Authentik, Keycloak, and Zitadel that implement standard OIDC
discovery and the authorization code flow with S256 PKCE. Provider compatibility
must be checked against the version you deploy.

## Register the application

Create a dedicated **confidential client** in your identity provider. Kallisti's
Go backend exchanges the code and keeps the client secret; the web browser does
not receive that secret. Enable PKCE with S256, allow `openid profile`, and
register this exact callback, substituting your Kallisti hostname:

```text
https://chat.example.org/api/oidc/callback
```

In Pocket ID, use Administration → OIDC Clients → Add OIDC Client. Give the
client a recognizable name, register the callback, and enable PKCE. Copy the
client ID and secret to your deployment's secret storage. Access to this client
should be restricted to the users you intend to admit.

Configure Kallisti:

```text
ALLOWED_ORIGINS=https://chat.example.org
OIDC_ISSUER=https://id.example.org
OIDC_CLIENT_ID=<client-id>
OIDC_CLIENT_SECRET_FILE=/run/secrets/kallisti-oidc
OIDC_NAME=Pocket ID
OIDC_REDIRECT_URL=https://chat.example.org/api/oidc/callback
```

The secret file contains only the client secret, with an optional trailing
newline. Mount it read-only and make it readable by the Kallisti process.
Alternatively use `OIDC_CLIENT_SECRET`; do not set both. Secrets belong in a
secret manager, Docker secret, or Kubernetes Secret, never a committed manifest
or image. The secret is loaded at startup; restart the process after rotation.

Both issuer and callback must use HTTPS. The callback origin must appear
explicitly in `ALLOWED_ORIGINS`; a wildcard does not qualify. Behind a trusted
reverse proxy that sets the forwarded headers, also set `TRUST_FORWARDED_FOR=1`
and block direct access to the application's HTTP port. This is the existing
cookie configuration requirement. OIDC start and callback requests are rejected
when the backend cannot identify the connection as secure.

The user's browser must reach both services. The Kallisti backend must reach
the provider's discovery, token, and signing-key endpoints. Restrictive
Kubernetes egress policies must permit DNS and those HTTPS destinations.
An operator-configured private HTTPS identity provider is allowed; this does
not change the public-address restrictions on user-supplied Fediverse servers.

## Account behavior

Choose the provider on the web login screen and authenticate there. A new
identity then chooses a local Kallisti name. Existing invitation requirements
and registration challenges still apply. Authenticating with a provider does
not bypass an invite-only server's admission policy.

Identities are keyed by the provider's exact issuer and subject ID. Names and
email addresses do not merge accounts. To retain an existing Kallisti account,
log into that account first and link the provider from account settings,
confirming the account password. An existing external-only account must first
set a local password through its current provider's reauthentication flow.

An OIDC-only account must authenticate with its linked provider again before
setting its first local password. Unlinking requires a confirmed local password
so the account retains a login method. Unlinking ends all other Kallisti sessions
and their active connections, retaining only the password-confirmed session. Changing the configured issuer does not
transfer identities: arrange another login method before replacing or removing
a provider.

The initial OIDC flow is available in the web app. Existing desktop/mobile QR
login and password login remain available; an OIDC account can set a local
password for native Mumble clients. Direct OIDC browser-to-native-app handoff
is outside this version's scope.

## Sessions and operations

Kallisti issues its own session after OIDC authentication. The provider's access
token is not used as a Kallisti session, and no refresh token is needed. Existing
Kallisti session lifetimes and logout controls continue to apply.

Disabling a user or ending their session at the identity provider does **not**
automatically end existing Kallisti sessions. This integration does not provide
group-to-role mapping, mandatory SSO, SCIM provisioning, or OIDC single logout.
Use Kallisti's own account and session controls when revoking existing access.

Provider discovery is deferred until login and cached after success. A provider
outage prevents new OIDC authentication but does not prevent Kallisti from
starting or existing password/Fediverse logins from working. Invalid local
configuration or an unreadable secret file is a startup error.

Before upgrading, back up the database, uploaded files, and certificates using
your normal deployment procedure. Keep a copy of the prior image and a consistent
data backup for rollback; do not assume older binaries can undo schema changes.

## Validation

Automated authentication tests use a local provider with signed ID tokens to
exercise code exchange, identity validation, browser binding, replay rejection,
account linking, and registration policy. Configuration tests cover the disabled
default, callback restrictions, and secret-file handling. Live acceptance should
also verify new-user signup, returning login, linking to an existing account,
first-password reauthentication, and continued password/guest/Fediverse access
with the deployed provider. First-password reauthentication is a required live
acceptance check: the provider must honor `prompt=login` and `max_age=0` and
return a fresh `auth_time` claim. Missing or stale authentication times fail
closed; without this support an OIDC-only user cannot set a password or unlink.
Verify this with the deployed Pocket ID version before admitting users.
