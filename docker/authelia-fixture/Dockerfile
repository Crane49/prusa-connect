# The Authelia fixture's image: Authelia with the sign-in check's
# configuration and people baked in (docker-services.md says why nothing is
# mounted), and keys made while the image is built, so no private key is kept
# in the repository: the RSA key Authelia requires, the P-256 key that signs
# this client's ID tokens with ES256, and the key and certificate of the TLS
# listener Authelia insists on.
#
# Pinned by digest. authelia/authelia 4.39.28 at the time of pinning.
FROM authelia/authelia:4.39.28@sha256:bd97cff4fcbf715b5ff1f9ae286afbe6033afce385302520b0368122d43a6f54

COPY config/configuration.yml config/users.yml /config/

# The certificate names 127.0.0.1, the address the browser and the server
# both reach the issuer at. scripts/check-oidc-providers.sh copies it out
# and hands it to the server as the one authority it trusts.
RUN mkdir -p /config/jwks /config/tls \
	&& authelia crypto pair rsa generate --bits 2048 --directory /config/jwks \
	&& authelia crypto pair ecdsa generate --curve P256 --directory /config/jwks \
		--file.private-key ec-private.pem --file.public-key ec-public.pem \
	&& authelia crypto certificate rsa generate --bits 2048 --common-name 127.0.0.1 \
		--sans 127.0.0.1 --directory /config/tls \
	&& chown -R 8000:8000 /config

ENV X_AUTHELIA_CONFIG=/config/configuration.yml \
	X_AUTHELIA_CONFIG_FILTERS=template
