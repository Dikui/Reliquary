package service

import (
	"errors"
	"unicode/utf8"
)

const MinimumPasswordLength = 8

var ErrInvalidPassword = errors.New("password must contain at least 8 characters and at most 72 bytes")

// ValidatePassword checks the minimum length and bcrypt input limit.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinimumPasswordLength || len(password) > 72 {
		return ErrInvalidPassword
	}
	return nil
}
