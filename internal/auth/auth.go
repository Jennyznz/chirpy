package auth

import (
	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"time"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"errors"
	"crypto/rand"
	"encoding/hex"
)

func GetAPIKey(headers http.Header) (string, error) {
	authHeader := headers.Get("Authorization")
	if (authHeader == "") {
		return "", errors.New("No auth header included in request")
	}

	parts := strings.Split(authHeader, " ")
	if (len(parts) < 2) {
		return "", errors.New("No api key")
	}

	return parts[1], nil
}

func MakeRefreshToken() string {
	data := make([]byte, 32)
	// Generate 32 bytes of random data
	rand.Read(data)
	// Convert random data into a hex string
	return hex.EncodeToString(data)
}

func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

func CheckPasswordHash(password, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	token := jwt.NewWithClaims(	// Create an unsigned token object
		jwt.SigningMethodHS256,	//	Tells library to use HMAC-SHA256 to sign the token
		jwt.RegisteredClaims{
			Issuer: "chirpy-access",
			IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(expiresIn)),
			Subject: userID.String(),
		})
	return token.SignedString([]byte(tokenSecret))	// Crytographically signs token with the scecret key
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claimsStruct := jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(
		tokenString,
		&claimsStruct,
		func(token *jwt.Token) (interface{}, error) {
			return []byte(tokenSecret), nil
		},
	)
	
	if err != nil {
		return uuid.Nil, err
	}

	subject := claimsStruct.Subject
	return uuid.Parse(subject)
}

func GetBearerToken(headers http.Header) (string, error) {
	authHeader := headers.Get("Authorization")
	if (authHeader == "") {
		return "", errors.New("No auth header included in request")
	}

	parts := strings.Split(authHeader, " ")
	if (len(parts) < 2) {
		return "", errors.New("No token string")
	}
	return parts[1], nil
}

