package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/jacobmw/external-dns-bluecat-webhook/internal/bluecat"
	"github.com/jacobmw/external-dns-bluecat-webhook/internal/config"
)

type mockClient struct {
	zones   []bluecat.Zone
	hosts   map[int64][]bluecat.HostRecord
	aliases map[int64][]bluecat.AliasRecord
	txts    map[int64][]bluecat.TXTRecord
	created []any
	deleted []int64
}

func (m *mockClient) ListZones(context.Context, string) ([]bluecat.Zone, error) {
	return m.zones, nil
}
func (m *mockClient) ListHostRecords(_ context.Context, zone bluecat.Zone) ([]bluecat.HostRecord, error) {
	return m.hosts[*zone.ID], nil
}
func (m *mockClient) ListAliasRecords(_ context.Context, zone bluecat.Zone) ([]bluecat.AliasRecord, error) {
	return m.aliases[*zone.ID], nil
}
func (m *mockClient) ListTXTRecords(_ context.Context, zone bluecat.Zone) ([]bluecat.TXTRecord, error) {
	return m.txts[*zone.ID], nil
}
func (m *mockClient) GetRecord(_ context.Context, absoluteName, recordType string) (*bluecat.GenericRecord, error) {
	id := *m.zones[0].ID
	switch recordType {
	case "HostRecord":
		for i := range m.hosts[id] {
			if deref(m.hosts[id][i].AbsoluteName) == absoluteName {
				return &bluecat.GenericRecord{ID: m.hosts[id][i].ID, Type: ptr("HostRecord"), AbsoluteName: m.hosts[id][i].AbsoluteName}, nil
			}
		}
	case "AliasRecord":
		for i := range m.aliases[id] {
			if deref(m.aliases[id][i].AbsoluteName) == absoluteName {
				return &bluecat.GenericRecord{ID: m.aliases[id][i].ID, Type: ptr("AliasRecord"), AbsoluteName: m.aliases[id][i].AbsoluteName}, nil
			}
		}
	case "TXTRecord":
		for i := range m.txts[id] {
			if deref(m.txts[id][i].AbsoluteName) == absoluteName {
				return &bluecat.GenericRecord{ID: m.txts[id][i].ID, Type: ptr("TXTRecord"), AbsoluteName: m.txts[id][i].AbsoluteName}, nil
			}
		}
	}
	return nil, nil
}
func (m *mockClient) CreateOrUpdateHost(_ context.Context, _ bluecat.Zone, rec bluecat.HostRecord) error {
	m.created = append(m.created, rec)
	return nil
}
func (m *mockClient) CreateOrUpdateAlias(_ context.Context, _ bluecat.Zone, rec bluecat.AliasRecord) error {
	m.created = append(m.created, rec)
	return nil
}
func (m *mockClient) CreateOrUpdateTXT(_ context.Context, _ bluecat.Zone, rec bluecat.TXTRecord) error {
	m.created = append(m.created, rec)
	return nil
}
func (m *mockClient) DeleteRecord(_ context.Context, id int64) error {
	m.deleted = append(m.deleted, id)
	return nil
}
func (m *mockClient) EnableDynamicUpdates(context.Context, []bluecat.Zone) error { return nil }
func (m *mockClient) DeployZone(context.Context, bluecat.Zone) error             { return nil }

func testProvider(client *mockClient) *Bluecat {
	return NewWithClient(&config.Config{
		DomainFilter:  []string{"example.com"},
		DNSDeployType: "no-deploy",
	}, client)
}

func TestRecords(t *testing.T) {
	zoneID := int64(42)
	ttl := int64(30)
	client := &mockClient{
		zones: []bluecat.Zone{{ID: &zoneID, AbsoluteName: ptr("example.com")}},
		hosts: map[int64][]bluecat.HostRecord{
			42: {{
				ID:           ptr(int64(1)),
				AbsoluteName: ptr("nginx.example.com"),
				TTL:          &ttl,
				Addresses:    []bluecat.Address{{Address: ptr("192.0.2.10")}, {Address: ptr("2001:db8::1")}},
			}},
		},
		aliases: map[int64][]bluecat.AliasRecord{
			42: {{
				ID:           ptr(int64(2)),
				AbsoluteName: ptr("www.example.com"),
				TTL:          &ttl,
				LinkedRecord: &bluecat.LinkedRecord{AbsoluteName: ptr("nginx.example.com")},
			}},
		},
		txts: map[int64][]bluecat.TXTRecord{
			42: {{
				ID:           ptr(int64(3)),
				AbsoluteName: ptr("nginx.example.com"),
				Text:         ptr("heritage=external-dns"),
			}},
		},
	}

	got, err := testProvider(client).Records(context.Background())
	require.NoError(t, err)

	want := []*endpoint.Endpoint{
		endpoint.NewEndpointWithTTL("nginx.example.com", endpoint.RecordTypeA, 30, "192.0.2.10"),
		endpoint.NewEndpointWithTTL("nginx.example.com", endpoint.RecordTypeAAAA, 30, "2001:db8::1"),
		endpoint.NewEndpointWithTTL("www.example.com", endpoint.RecordTypeCNAME, 30, "nginx.example.com"),
		endpoint.NewEndpoint("nginx.example.com", endpoint.RecordTypeTXT, "heritage=external-dns"),
	}
	assert.True(t, sameEndpoints(got, want), "got %#v want %#v", got, want)
}

func TestApplyChangesCreateAndDelete(t *testing.T) {
	zoneID := int64(42)
	client := &mockClient{
		zones: []bluecat.Zone{{ID: &zoneID, AbsoluteName: ptr("example.com")}},
		hosts: map[int64][]bluecat.HostRecord{
			42: {{ID: ptr(int64(9)), AbsoluteName: ptr("old.example.com"), Addresses: []bluecat.Address{{Address: ptr("192.0.2.9")}}}},
		},
		aliases: map[int64][]bluecat.AliasRecord{},
		txts:    map[int64][]bluecat.TXTRecord{},
	}

	err := testProvider(client).ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.NewEndpoint("app.example.com", endpoint.RecordTypeA, "192.0.2.20"),
			endpoint.NewEndpoint("alias.example.com", endpoint.RecordTypeCNAME, "app.example.com"),
			endpoint.NewEndpoint("app.example.com", endpoint.RecordTypeTXT, "owned"),
		},
		Delete: []*endpoint.Endpoint{
			endpoint.NewEndpoint("old.example.com", endpoint.RecordTypeA, "192.0.2.9"),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []int64{9}, client.deleted)
	require.Len(t, client.created, 3)
}

func TestFindZonePrefersMostSpecific(t *testing.T) {
	parent := bluecat.Zone{ID: ptr(int64(1)), AbsoluteName: ptr("example.com")}
	child := bluecat.Zone{ID: ptr(int64(2)), AbsoluteName: ptr("dev.example.com")}
	got := findZone([]bluecat.Zone{parent, child}, "api.dev.example.com")
	require.NotNil(t, got)
	assert.Equal(t, int64(2), *got.ID)
}

func sameEndpoints(a, b []*endpoint.Endpoint) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, left := range a {
		found := false
		for i, right := range b {
			if used[i] {
				continue
			}
			if left.DNSName == right.DNSName && left.RecordType == right.RecordType && left.Targets.Same(right.Targets) {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
