package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	log "github.com/sirupsen/logrus"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"

	"github.com/jacobmw/external-dns-bluecat-webhook/internal/bluecat"
	"github.com/jacobmw/external-dns-bluecat-webhook/internal/config"
)

const (
	recordHost  = "HostRecord"
	recordAlias = "AliasRecord"
	recordTXT   = "TXTRecord"
)

// Bluecat implements provider.Provider against Address Manager API v2.
type Bluecat struct {
	provider.BaseProvider
	client        bluecat.Client
	domainFilter  *endpoint.DomainFilter
	zoneIDFilter  provider.ZoneIDFilter
	dryRun        bool
	rootZone      string
	view          string
	dnsServerName string
	dnsDeployType string
}

// New builds a provider from config using a live BAM client.
func New(ctx context.Context, cfg *config.Config) (*Bluecat, error) {
	client, err := bluecat.Login(ctx, bluecat.Config{
		Host:          cfg.Host,
		Username:      cfg.Username,
		Password:      cfg.Password,
		SkipTLSVerify: cfg.SkipTLSVerify,
		CAFile:        cfg.CAFile,
		Timeout:       cfg.HTTPClientTimeout,
	})
	if err != nil {
		return nil, err
	}
	return NewWithClient(cfg, client), nil
}

// NewWithClient is used by tests.
func NewWithClient(cfg *config.Config, client bluecat.Client) *Bluecat {
	return &Bluecat{
		client:        client,
		domainFilter:  endpoint.NewDomainFilterWithExclusions(cfg.DomainFilter, cfg.ExcludeDomains),
		zoneIDFilter:  provider.NewZoneIDFilter(cfg.ZoneIDFilter),
		dryRun:        cfg.DryRun,
		rootZone:      cfg.RootZone,
		view:          cfg.View,
		dnsServerName: cfg.DNSServerName,
		dnsDeployType: cfg.DNSDeployType,
	}
}

// GetDomainFilter returns the webhook negotiation filter.
func (p *Bluecat) GetDomainFilter() endpoint.DomainFilterInterface {
	return p.domainFilter
}

// Records lists A, AAAA, CNAME, and TXT records from matching zones.
func (p *Bluecat) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	zones, err := p.zones(ctx)
	if err != nil {
		return nil, err
	}

	var endpoints []*endpoint.Endpoint
	for _, zone := range zones {
		hosts, err := p.client.ListHostRecords(ctx, zone)
		if err != nil {
			return nil, fmt.Errorf("list host records in %s: %w", zone.AbsoluteNameOrEmpty(), err)
		}
		for _, rec := range hosts {
			name := deref(rec.AbsoluteName)
			v4, v6 := splitAddresses(rec.IPAddresses())
			if len(v4) > 0 {
				endpoints = append(endpoints, hostEndpoint(name, endpoint.RecordTypeA, rec.TTL, rec.Comment, v4...))
			}
			if len(v6) > 0 {
				endpoints = append(endpoints, hostEndpoint(name, endpoint.RecordTypeAAAA, rec.TTL, rec.Comment, v6...))
			}
		}

		aliases, err := p.client.ListAliasRecords(ctx, zone)
		if err != nil {
			return nil, fmt.Errorf("list alias records in %s: %w", zone.AbsoluteNameOrEmpty(), err)
		}
		for _, rec := range aliases {
			if rec.Target() == "" {
				continue
			}
			endpoints = append(endpoints, hostEndpoint(deref(rec.AbsoluteName), endpoint.RecordTypeCNAME, rec.TTL, rec.Comment, rec.Target()))
		}

		txts, err := p.client.ListTXTRecords(ctx, zone)
		if err != nil {
			return nil, fmt.Errorf("list txt records in %s: %w", zone.AbsoluteNameOrEmpty(), err)
		}
		for _, rec := range txts {
			endpoints = append(endpoints, hostEndpoint(deref(rec.AbsoluteName), endpoint.RecordTypeTXT, rec.TTL, rec.Comment, deref(rec.Text)))
		}
	}

	log.Debugf("fetched %d records from BlueCat", len(endpoints))
	return endpoints, nil
}

// ApplyChanges creates and deletes records, then optionally deploys zones.
func (p *Bluecat) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	zones, err := p.zones(ctx)
	if err != nil {
		return err
	}
	created, deleted := p.mapChanges(zones, changes)

	if err := p.deleteRecords(ctx, deleted); err != nil {
		return err
	}
	if err := p.createRecords(ctx, zones, created); err != nil {
		return err
	}
	return p.deploy(ctx, zones)
}

func (p *Bluecat) zones(ctx context.Context) ([]bluecat.Zone, error) {
	all, err := p.client.ListZones(ctx, p.rootZone)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	if p.dnsDeployType == "dynamic" {
		if err := p.client.EnableDynamicUpdates(ctx, all); err != nil {
			return nil, err
		}
	}

	var zones []bluecat.Zone
	for _, zone := range all {
		name := zone.AbsoluteNameOrEmpty()
		if name == "" {
			name = deref(zone.Name)
		}
		if !p.domainFilter.Match(name) {
			continue
		}
		if p.view != "" && zone.View != nil && !strings.EqualFold(*zone.View, p.view) {
			continue
		}
		if !p.zoneIDFilter.Match(zone.IDString()) {
			continue
		}
		zones = append(zones, zone)
	}
	log.Debugf("using %d of %d BlueCat zones", len(zones), len(all))
	return zones, nil
}

type changeMap map[int64][]*endpoint.Endpoint

func (p *Bluecat) mapChanges(zones []bluecat.Zone, changes *plan.Changes) (changeMap, changeMap) {
	created := changeMap{}
	deleted := changeMap{}
	assign := func(m changeMap, ep *endpoint.Endpoint) {
		zone := findZone(zones, ep.DNSName)
		if zone == nil || zone.ID == nil {
			log.Debugf("ignoring %s %s: no matching BlueCat zone", ep.RecordType, ep.DNSName)
			return
		}
		m[*zone.ID] = append(m[*zone.ID], ep)
	}
	for _, ep := range changes.Delete {
		assign(deleted, ep)
	}
	for _, ep := range changes.UpdateOld {
		assign(deleted, ep)
	}
	for _, ep := range changes.Create {
		assign(created, ep)
	}
	for _, ep := range changes.UpdateNew {
		assign(created, ep)
	}
	return created, deleted
}

func findZone(zones []bluecat.Zone, name string) *bluecat.Zone {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	var best *bluecat.Zone
	bestLen := -1
	for i := range zones {
		zname := strings.TrimSuffix(strings.ToLower(zones[i].AbsoluteNameOrEmpty()), ".")
		if zname == "" {
			continue
		}
		if name == zname || strings.HasSuffix(name, "."+zname) {
			if len(zname) > bestLen {
				best = &zones[i]
				bestLen = len(zname)
			}
		}
	}
	return best
}

func (p *Bluecat) createRecords(ctx context.Context, zones []bluecat.Zone, created changeMap) error {
	byID := map[int64]bluecat.Zone{}
	for _, z := range zones {
		if z.ID != nil {
			byID[*z.ID] = z
		}
	}
	for id, endpoints := range created {
		zone, ok := byID[id]
		if !ok {
			continue
		}
		for _, ep := range endpoints {
			if p.dryRun {
				log.Infof("would create %s %s -> %s", ep.RecordType, ep.DNSName, ep.Targets)
				continue
			}
			log.Infof("creating %s %s -> %s", ep.RecordType, ep.DNSName, ep.Targets)
			if err := p.upsertEndpoint(ctx, zone, ep); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Bluecat) deleteRecords(ctx context.Context, deleted changeMap) error {
	for _, endpoints := range deleted {
		for _, ep := range endpoints {
			if p.dryRun {
				log.Infof("would delete %s %s", ep.RecordType, ep.DNSName)
				continue
			}
			log.Infof("deleting %s %s", ep.RecordType, ep.DNSName)
			recType := bluecatType(ep.RecordType)
			if recType == "" {
				log.Debugf("skipping unsupported type %s", ep.RecordType)
				continue
			}
			existing, err := p.client.GetRecord(ctx, strings.TrimSuffix(ep.DNSName, "."), recType)
			if err != nil {
				return fmt.Errorf("lookup %s %s: %w", ep.RecordType, ep.DNSName, err)
			}
			if existing == nil || existing.ID == nil {
				log.Debugf("no existing record for %s %s", ep.RecordType, ep.DNSName)
				continue
			}
			if err := p.client.DeleteRecord(ctx, *existing.ID); err != nil {
				return fmt.Errorf("delete %s %s: %w", ep.RecordType, ep.DNSName, err)
			}
		}
	}
	return nil
}

func (p *Bluecat) upsertEndpoint(ctx context.Context, zone bluecat.Zone, ep *endpoint.Endpoint) error {
	recType := bluecatType(ep.RecordType)
	existing, err := p.client.GetRecord(ctx, strings.TrimSuffix(ep.DNSName, "."), recType)
	if err != nil {
		return err
	}
	var id *int64
	if existing != nil {
		id = existing.ID
	}
	comment := labelsJSON(ep)
	name := strings.TrimSuffix(ep.DNSName, ".")
	var ttl *int64
	if ep.RecordTTL.IsConfigured() {
		ttl = ptr(int64(ep.RecordTTL))
	}

	switch ep.RecordType {
	case endpoint.RecordTypeA, endpoint.RecordTypeAAAA:
		addrs := make([]bluecat.Address, 0, len(ep.Targets))
		for _, t := range ep.Targets {
			addrs = append(addrs, bluecat.Address{Address: ptr(t)})
		}
		return p.client.CreateOrUpdateHost(ctx, zone, bluecat.HostRecord{
			ID:           id,
			AbsoluteName: &name,
			TTL:          ttl,
			Comment:      comment,
			Addresses:    addrs,
		})
	case endpoint.RecordTypeCNAME:
		if len(ep.Targets) == 0 {
			return fmt.Errorf("cname %s has no target", ep.DNSName)
		}
		return p.client.CreateOrUpdateAlias(ctx, zone, bluecat.AliasRecord{
			ID:           id,
			AbsoluteName: &name,
			TTL:          ttl,
			Comment:      comment,
			LinkedRecord: &bluecat.LinkedRecord{AbsoluteName: ptr(strings.TrimSuffix(ep.Targets[0], "."))},
		})
	case endpoint.RecordTypeTXT:
		text := ""
		if len(ep.Targets) > 0 {
			text = ep.Targets[0]
		}
		return p.client.CreateOrUpdateTXT(ctx, zone, bluecat.TXTRecord{
			ID:           id,
			AbsoluteName: &name,
			TTL:          ttl,
			Comment:      comment,
			Text:         &text,
		})
	default:
		log.Debugf("skipping unsupported type %s for %s", ep.RecordType, ep.DNSName)
		return nil
	}
}

func (p *Bluecat) deploy(ctx context.Context, zones []bluecat.Zone) error {
	if p.dnsServerName == "" || p.dnsDeployType != "quick-deploy" {
		return nil
	}
	for _, zone := range zones {
		if p.dryRun {
			log.Infof("would deploy zone %s", zone.AbsoluteNameOrEmpty())
			continue
		}
		if err := p.client.DeployZone(ctx, zone); err != nil {
			return err
		}
		log.Infof("deployed zone %s", zone.AbsoluteNameOrEmpty())
	}
	return nil
}

func hostEndpoint(name, rtype string, ttl *int64, comment *string, targets ...string) *endpoint.Endpoint {
	var ep *endpoint.Endpoint
	if ttl != nil {
		ep = endpoint.NewEndpointWithTTL(name, rtype, endpoint.TTL(*ttl), targets...)
	} else {
		ep = endpoint.NewEndpoint(name, rtype, targets...)
	}
	if comment != nil && *comment != "" {
		var labels endpoint.Labels
		if err := json.Unmarshal([]byte(*comment), &labels); err == nil {
			ep.Labels = labels
		}
	}
	return ep
}

func splitAddresses(ips []string) (v4, v6 []string) {
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		if parsed.To4() != nil {
			v4 = append(v4, ip)
		} else {
			v6 = append(v6, ip)
		}
	}
	return v4, v6
}

func bluecatType(recordType string) string {
	switch recordType {
	case endpoint.RecordTypeA, endpoint.RecordTypeAAAA:
		return recordHost
	case endpoint.RecordTypeCNAME:
		return recordAlias
	case endpoint.RecordTypeTXT:
		return recordTXT
	default:
		return ""
	}
}

func labelsJSON(ep *endpoint.Endpoint) *string {
	if len(ep.Labels) == 0 {
		return nil
	}
	b, err := json.Marshal(ep.Labels)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ptr[T any](v T) *T { return &v }
