package jwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"time"

	jose "gopkg.in/square/go-jose.v2"
	jwt "gopkg.in/square/go-jose.v2/jwt"
)

type privateClaims struct {
	Username  string `json:"username,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
}

type JSONWebToken struct {
	NotBefore time.Time
	IssuedAt  time.Time
	Expiry    time.Time
	Username  string
	SecretKey string
}

func UnmarshalRSAKey(data string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(data))
	if block == nil {
		return nil, errors.New("Failed to decode PEM data")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("key is not of type *rsa.PrivateKey")
	}

	return rsaKey, nil
}

func Build(key *rsa.PrivateKey, username string, secretKey string) (string, error) {
	var token string

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return token, err
	}

	recipient := jose.Recipient{
		Algorithm: jose.RSA_OAEP_256,
		Key:       key.Public(),
	}

	encrypter, err := jose.NewEncrypter(jose.A128CBC_HS256, recipient, (&jose.EncrypterOptions{}).WithContentType("JWT").WithType("JWT"))
	if err != nil {
		return token, err
	}

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(time.Duration(12) * time.Hour)

	claims := jwt.Claims{
		NotBefore: jwt.NewNumericDate(issuedAt),
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		Expiry:    jwt.NewNumericDate(expiresAt),
	}

	privateClaims := privateClaims{
		Username:  username,
		SecretKey: secretKey,
	}

	token, err = jwt.SignedAndEncrypted(signer, encrypter).Claims(claims).Claims(privateClaims).CompactSerialize()
	if err != nil {
		return token, err
	}

	return token, nil
}

func Parse(key *rsa.PrivateKey, rawToken string) (JSONWebToken, error) {
	var token JSONWebToken
	var claims jwt.Claims
	var privateClaims privateClaims

	encryptedToken, err := jwt.ParseSignedAndEncrypted(rawToken)
	if err != nil {
		return token, err
	}

	decryptedToken, err := encryptedToken.Decrypt(key)
	if err != nil {
		return token, err
	}

	if err := decryptedToken.Claims(key.Public(), &claims, &privateClaims); err != nil {
		return token, err
	}

	token.Expiry = claims.Expiry.Time()
	token.IssuedAt = claims.IssuedAt.Time()
	token.NotBefore = claims.NotBefore.Time()
	token.Username = privateClaims.Username
	token.SecretKey = privateClaims.SecretKey

	return token, nil
}
