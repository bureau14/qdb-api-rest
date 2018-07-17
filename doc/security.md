## HTTPS requirement

The REST API sets CSP and XSS protection headers to enforce TLS. It shall respond with a 400 status code to any 
non-HTTPS request.

    Content-Security-Policy: default-src https: data: 'unsafe-inline' 'unsafe-eval'
    X-Frame-Options: SAMEORIGIN
    X-XSS-Protection: 1; mode=block

Cipher suites for TLS1.3 shall be activated and TLS1.2 disabled.

## Client-side authentication & authorization

The API holds a copy of the database server's public key.
The API exposes a `/login` endpoint, which expects a QuasarDB user's private key as body, and returns a
JSON Web Token (JWT).
This JWT is signed and encrypted with a key only known to the API and contains at least the user's private key and an
expiry date. It might also encode other metadata as required by the API.

The client then must pass this token along in the `Authorization` header of every request as follows:

    Authorization: Bearer <jwt token>
    
The client shall only save the token in memory, not the original private key.

The API must verify the token with each request and, if valid, use the contained private key to perform requests to
the database server.
If the key is invalid or expired, the API should respond with a 401 HTTP status code, and the
frontend shall restart the authorization process.