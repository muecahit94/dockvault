package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/muecahit94/dockvault/models"
)

func TestParseAzureConnectionString(t *testing.T) {
	url, acct, key, ok := parseAzureConnectionString(
		"DefaultEndpointsProtocol=https;AccountName=mybackups;AccountKey=dGVzdGtleQ==;EndpointSuffix=core.windows.net",
	)
	if !ok || url != "https://mybackups.blob.core.windows.net" || acct != "mybackups" || key != "dGVzdGtleQ==" {
		t.Fatalf("unexpected parse result: ok=%v url=%q acct=%q", ok, url, acct)
	}

	url2, acct2, _, ok2 := parseAzureConnectionString(
		"DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;",
	)
	if !ok2 || url2 != "http://127.0.0.1:10000/devstoreaccount1" || acct2 != "devstoreaccount1" {
		t.Fatalf("unexpected custom BlobEndpoint parse: ok=%v url=%q acct=%q", ok2, url2, acct2)
	}

	if _, _, _, ok3 := parseAzureConnectionString("UseDevelopmentStorage=true"); ok3 {
		t.Fatalf("expected UseDevelopmentStorage shorthand to delegate to azblob connection string parser")
	}
}

func TestParseAzureAccessTier(t *testing.T) {
	cases := map[string]blob.AccessTier{
		"":        blob.AccessTierCool,
		"hot":     blob.AccessTierHot,
		"Cool":    blob.AccessTierCool,
		"COLD":    blob.AccessTierCold,
		"archive": blob.AccessTierArchive,
	}
	for input, want := range cases {
		got, err := parseAzureAccessTier(input)
		if err != nil || got != want {
			t.Fatalf("parseAzureAccessTier(%q) = (%q, %v), want %q", input, got, err, want)
		}
	}

	if _, err := parseAzureAccessTier("premium"); err == nil {
		t.Fatalf("expected invalid access tier error")
	}
}

func TestAzureDriverSASGeneration(t *testing.T) {
	driver, err := NewAzureDriver(models.StorageProvider{
		Type:             "azure",
		AzureAuthType:    "key",
		AzureAccountName: "myacct",
		AzureContainer:   "dockvault",
		AzureAccessTier:  "Cool",
		Prefix:           "homelab",
		SecretKey:        "dGVzdGtleXRlc3RrZXl0ZXN0a2V5dGVzdGtleXRlc3Q=",
	})
	if err != nil {
		t.Fatalf("NewAzureDriver failed: %v", err)
	}

	sasURL, err := driver.GenerateSASURL(context.Background(), "homelab/pg-20261117.tar.gz", time.Hour)
	if err != nil {
		t.Fatalf("GenerateSASURL failed: %v", err)
	}
	if !strings.HasPrefix(sasURL, "https://myacct.blob.core.windows.net/dockvault/homelab/pg-20261117.tar.gz?") ||
		!strings.Contains(sasURL, "sig=") || !strings.Contains(sasURL, "sp=r") {
		t.Fatalf("unexpected SAS URL format: %s", sasURL)
	}

	// SAS-token-only driver cannot re-sign SAS URLs without an account key.
	sasOnlyDriver, err := NewAzureDriver(models.StorageProvider{
		Type:             "azure",
		AzureAuthType:    "sas",
		AzureAccountName: "myacct",
		AzureContainer:   "dockvault",
		SecretKey:        "?sv=2024-08-04&ss=b&srt=co&sp=rwdlac&se=2030-01-01T00:00:00Z&sig=abc",
	})
	if err != nil {
		t.Fatalf("NewAzureDriver sas mode failed: %v", err)
	}
	if _, err := sasOnlyDriver.GenerateSASURL(context.Background(), "homelab/pg.tar.gz", time.Hour); err == nil {
		t.Fatalf("expected error when generating SAS URL from SAS-only provider")
	}
}

func TestAzureDriverServicePrincipalInitAndValidation(t *testing.T) {
	// Missing tenant/client/secret must fail upfront.
	if _, err := NewAzureDriver(models.StorageProvider{
		Type:             "azure",
		AzureAuthType:    "service_principal",
		AzureAccountName: "myacct",
		AzureContainer:   "dockvault",
	}); err == nil {
		t.Fatalf("expected validation error for incomplete service principal config")
	}

	driver, err := NewAzureDriver(models.StorageProvider{
		Type:             "azure",
		AzureAuthType:    "service_principal",
		AzureAccountName: "myacct",
		AzureTenantID:    "11111111-1111-1111-1111-111111111111",
		AzureClientID:    "22222222-2222-2222-2222-222222222222",
		AzureContainer:   "dockvault",
		SecretKey:        "super-sp-client-secret",
	})
	if err != nil {
		t.Fatalf("NewAzureDriver service_principal failed: %v", err)
	}
	if !driver.canGenerateSAS || !driver.useUserDelegationSAS {
		t.Fatalf("expected service_principal driver to support User Delegation SAS")
	}
}
