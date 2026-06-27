package auth

import (
	"time"
	"github.com/google/uuid"
	"testing"
)

func TestValidateJWT(t *testing.T) {
	// Set up data
	userID := uuid.New()
	validToken, err := MakeJWT(userID, "secret", time.Hour)
	if err != nil {
    t.Fatalf("failed to make valid token: %v", err)
	}

	// Define test cases
	tests := []struct {
		name string
		tokenString string
		tokenSecret string
		wantUserID uuid.UUID
		wantErr bool
	} {
		{
			name: "Valid token",
			tokenString: validToken,
			tokenSecret: "secret",
			wantUserID: userID,
			wantErr: false,
		},
	}

	// Run tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUserID, err := ValidateJWT(tt.tokenString, tt.tokenSecret)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateJWT() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotUserID != tt.wantUserID {
				t.Errorf("ValidateJWT() gotUserID = %v, want %v", gotUserID, tt.wantUserID)
			}
		})
	}

}