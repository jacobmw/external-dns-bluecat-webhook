package bluecat

import "strconv"

// Session is the BlueCat Address Manager v2 login payload and response.
type Session struct {
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
	APIToken *string `json:"apiToken,omitempty"`
}

type collection[T any] struct {
	Count      *int64 `json:"count,omitempty"`
	TotalCount *int64 `json:"totalCount,omitempty"`
	Data       []T    `json:"data,omitempty"`
}

// Zone is a DNS zone in Address Manager.
type Zone struct {
	ID                    *int64  `json:"id,omitempty"`
	Type                  *string `json:"type,omitempty"`
	Name                  *string `json:"name,omitempty"`
	AbsoluteName          *string `json:"absoluteName,omitempty"`
	DynamicUpdateEnabled  *bool   `json:"dynamicUpdateEnabled,omitempty"`
	DeploymentEnabled     *bool   `json:"deploymentEnabled,omitempty"`
	View                  *string `json:"view,omitempty"`
}

func (z Zone) AbsoluteNameOrEmpty() string {
	if z.AbsoluteName == nil {
		return ""
	}
	return *z.AbsoluteName
}

func (z Zone) IDString() string {
	if z.ID == nil {
		return ""
	}
	return strconv.FormatInt(*z.ID, 10)
}

// GenericRecord is a type-agnostic resource record used for lookups.
type GenericRecord struct {
	ID           *int64  `json:"id,omitempty"`
	Type         *string `json:"type,omitempty"`
	Name         *string `json:"name,omitempty"`
	AbsoluteName *string `json:"absoluteName,omitempty"`
	Comment      *string `json:"comment,omitempty"`
	TTL          *int64  `json:"ttl,omitempty"`
}

// Address is an IPv4 or IPv6 address attached to a host record.
type Address struct {
	ID      *int64  `json:"id,omitempty"`
	Type    *string `json:"type,omitempty"`
	Address *string `json:"address,omitempty"`
}

// HostRecord is a BlueCat HostRecord (A/AAAA).
type HostRecord struct {
	ID           *int64     `json:"id,omitempty"`
	Type         string     `json:"type"`
	Name         *string    `json:"name,omitempty"`
	AbsoluteName *string    `json:"absoluteName,omitempty"`
	Comment      *string    `json:"comment,omitempty"`
	TTL          *int64     `json:"ttl,omitempty"`
	Addresses    []Address  `json:"addresses,omitempty"`
	Embedded     *embeddedAddresses `json:"_embedded,omitempty"`
}

type embeddedAddresses struct {
	Addresses []Address `json:"addresses,omitempty"`
}

func (r HostRecord) IPAddresses() []string {
	seen := map[string]struct{}{}
	var ips []string
	add := func(list []Address) {
		for _, a := range list {
			if a.Address == nil || *a.Address == "" {
				continue
			}
			if _, ok := seen[*a.Address]; ok {
				continue
			}
			seen[*a.Address] = struct{}{}
			ips = append(ips, *a.Address)
		}
	}
	add(r.Addresses)
	if r.Embedded != nil {
		add(r.Embedded.Addresses)
	}
	return ips
}

// AliasRecord is a BlueCat CNAME (AliasRecord).
type AliasRecord struct {
	ID           *int64         `json:"id,omitempty"`
	Type         string         `json:"type"`
	Name         *string        `json:"name,omitempty"`
	AbsoluteName *string        `json:"absoluteName,omitempty"`
	Comment      *string        `json:"comment,omitempty"`
	TTL          *int64         `json:"ttl,omitempty"`
	LinkedRecord *LinkedRecord  `json:"linkedRecord,omitempty"`
}

// LinkedRecord is the CNAME target, either an existing record or an external host.
type LinkedRecord struct {
	ID           *int64  `json:"id,omitempty"`
	Type         *string `json:"type,omitempty"`
	AbsoluteName *string `json:"absoluteName,omitempty"`
}

func (r AliasRecord) Target() string {
	if r.LinkedRecord == nil || r.LinkedRecord.AbsoluteName == nil {
		return ""
	}
	return *r.LinkedRecord.AbsoluteName
}

// TXTRecord is a BlueCat TXT record.
type TXTRecord struct {
	ID           *int64  `json:"id,omitempty"`
	Type         string  `json:"type"`
	Name         *string `json:"name,omitempty"`
	AbsoluteName *string `json:"absoluteName,omitempty"`
	Comment      *string `json:"comment,omitempty"`
	TTL          *int64  `json:"ttl,omitempty"`
	Text         *string `json:"text,omitempty"`
}

type quickDeployment struct {
	Type string `json:"type"`
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ptr[T any](v T) *T {
	return &v
}
