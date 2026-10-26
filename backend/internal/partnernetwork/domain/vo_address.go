package domain

import (
	"strings"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// newAddress valida la dirección entrante reutilizando kernel.Address.
func newAddress(in AddressInput) (kernel.Address, error) {
	ve := &kernel.ValidationError{}
	c, err := kernel.NewCountryCode(in.Country)
	if err != nil {
		ve.Add("address.country", "INVALID")
	}
	city := strings.TrimSpace(in.City)
	if city == "" || len([]rune(city)) > 120 {
		ve.Add("address.city", "INVALID")
	}
	line1 := strings.TrimSpace(in.Line1)
	if line1 == "" || len([]rune(line1)) > 200 {
		ve.Add("address.line1", "INVALID")
	}
	region := strings.TrimSpace(deref(in.Region))
	if len(region) > 120 {
		ve.Add("address.region", "INVALID")
	}
	line2 := strings.TrimSpace(deref(in.Line2))
	if len(line2) > 200 {
		ve.Add("address.line2", "INVALID")
	}
	postal := strings.TrimSpace(deref(in.PostalCode))
	if len(postal) > 20 {
		ve.Add("address.postal_code", "INVALID")
	}
	if err := ve.ErrOrNil(); err != nil {
		return kernel.Address{}, err
	}
	return kernel.Address{
		Country: c, Region: region, City: city, Line1: line1, Line2: line2, PostalCode: postal,
	}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func invalid(field string) error {
	return &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: field, Code: "INVALID"}}}
}
