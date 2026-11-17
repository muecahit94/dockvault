package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/muecahit94/dockvault/models"
)

// ErrAzureArchiveTier indicates a blob cannot be downloaded directly because it is in Archive tier.
var ErrAzureArchiveTier = errors.New("blob is in Azure Archive tier and must be rehydrated before downloading")

// AzureDriver stores backup archives in an Azure Blob Storage container.
type AzureDriver struct {
	client               *azblob.Client
	sharedKeyCred        *azblob.SharedKeyCredential
	useUserDelegationSAS bool
	serviceURL           string
	container            string
	prefix               string
	accessTier           blob.AccessTier
	autoCreateContainer  bool
	canGenerateSAS       bool
}

// NewAzureDriver initializes an AzureDriver using Shared Key, SAS token, or Connection String.
func NewAzureDriver(provider models.StorageProvider) (*AzureDriver, error) {
	containerName := strings.TrimSpace(provider.AzureContainer)
	if containerName == "" {
		containerName = strings.TrimSpace(provider.Bucket)
	}
	if containerName == "" {
		return nil, fmt.Errorf("Azure blob container name is required")
	}

	authType := strings.ToLower(strings.TrimSpace(provider.AzureAuthType))
	if authType == "" {
		authType = "key"
	}

	secret := strings.TrimSpace(provider.SecretKey)
	accountName := strings.TrimSpace(provider.AzureAccountName)
	endpoint := strings.TrimRight(strings.TrimSpace(provider.Endpoint), "/")

	var (
		client               *azblob.Client
		sharedKeyCred        *azblob.SharedKeyCredential
		resolvedURL          string
		canGenerateSAS       bool
		useUserDelegationSAS bool
		err                  error
	)

	switch authType {
	case "key":
		if accountName == "" || secret == "" {
			return nil, fmt.Errorf("Azure account name and account key are required")
		}
		resolvedURL = resolveAzureServiceURL(accountName, endpoint)
		cred, credErr := azblob.NewSharedKeyCredential(accountName, secret)
		if credErr != nil {
			return nil, fmt.Errorf("invalid Azure shared key credentials: %w", credErr)
		}
		client, err = azblob.NewClientWithSharedKeyCredential(resolvedURL, cred, nil)
		sharedKeyCred = cred
		canGenerateSAS = true

	case "service_principal":
		tenantID := strings.TrimSpace(provider.AzureTenantID)
		clientID := strings.TrimSpace(provider.AzureClientID)
		if tenantID == "" || clientID == "" || secret == "" {
			return nil, fmt.Errorf("Azure tenantId, clientId, and client secret are required for service principal auth")
		}
		if accountName == "" && endpoint == "" {
			return nil, fmt.Errorf("Azure account name or custom endpoint is required")
		}
		resolvedURL = resolveAzureServiceURL(accountName, endpoint)
		cred, credErr := azidentity.NewClientSecretCredential(tenantID, clientID, secret, nil)
		if credErr != nil {
			return nil, fmt.Errorf("invalid Azure service principal credentials: %w", credErr)
		}
		client, err = azblob.NewClient(resolvedURL, cred, nil)
		canGenerateSAS = true
		useUserDelegationSAS = true

	case "connection_string":
		if secret == "" {
			return nil, fmt.Errorf("Azure storage connection string is required")
		}
		if parsedURL, parsedAcct, parsedKey, ok := parseAzureConnectionString(secret); ok {
			cred, credErr := azblob.NewSharedKeyCredential(parsedAcct, parsedKey)
			if credErr == nil {
				client, err = azblob.NewClientWithSharedKeyCredential(parsedURL, cred, nil)
				resolvedURL = parsedURL
				sharedKeyCred = cred
				canGenerateSAS = true
			}
		}
		if client == nil && err == nil {
			client, err = azblob.NewClientFromConnectionString(secret, nil)
		}

	case "sas":
		if secret == "" {
			return nil, fmt.Errorf("Azure SAS token is required")
		}
		fullURL, buildErr := buildAzureSASServiceURL(accountName, endpoint, secret)
		if buildErr != nil {
			return nil, buildErr
		}
		client, err = azblob.NewClientWithNoCredential(fullURL, nil)
		canGenerateSAS = false

	default:
		return nil, fmt.Errorf("unsupported Azure auth type %q (expected key, service_principal, connection_string, or sas)", authType)
	}

	if err != nil {
		return nil, fmt.Errorf("initialize Azure blob client: %w", err)
	}

	tier, err := parseAzureAccessTier(provider.AzureAccessTier)
	if err != nil {
		return nil, err
	}

	return &AzureDriver{
		client:               client,
		sharedKeyCred:        sharedKeyCred,
		useUserDelegationSAS: useUserDelegationSAS,
		serviceURL:           strings.TrimRight(resolvedURL, "/"),
		container:            containerName,
		prefix:               strings.Trim(strings.TrimSpace(provider.Prefix), "/"),
		accessTier:           tier,
		autoCreateContainer:  provider.AzureAutoCreateContainer,
		canGenerateSAS:       canGenerateSAS,
	}, nil
}

func (d *AzureDriver) Test(ctx context.Context) error {
	_, err := d.TestWithDetails(ctx)
	return err
}

// TestWithDetails verifies container reachability, optionally creates it, and summarizes stored blobs.
func (d *AzureDriver) TestWithDetails(ctx context.Context) (*TestResult, error) {
	created, err := d.ensureContainer(ctx)
	if err != nil {
		return nil, err
	}

	res := &TestResult{
		ContainerCreated: created,
		Message:          fmt.Sprintf("Connected to Azure container %q", d.container),
	}
	if created {
		res.Message = fmt.Sprintf("Created and connected to Azure container %q", d.container)
	}

	// Best-effort blob count/size summary (write-only SAS tokens may not have List permission).
	var prefixPtr *string
	if d.prefix != "" {
		p := d.prefix + "/"
		prefixPtr = &p
	}
	pager := d.client.NewListBlobsFlatPager(d.container, &azblob.ListBlobsFlatOptions{
		Prefix: prefixPtr,
	})
	if pager.More() {
		page, listErr := pager.NextPage(ctx)
		if listErr == nil && page.Segment != nil {
			count := len(page.Segment.BlobItems)
			var total int64
			for _, item := range page.Segment.BlobItems {
				if item.Properties != nil && item.Properties.ContentLength != nil {
					total += *item.Properties.ContentLength
				}
			}
			res.ObjectCount = &count
			res.TotalBytes = &total
		}
	}

	return res, nil
}

func (d *AzureDriver) Save(ctx context.Context, localTempPath, fileName string) (string, error) {
	safeName, err := SafeFileName(fileName)
	if err != nil {
		return "", err
	}

	if d.autoCreateContainer {
		if _, err := d.ensureContainer(ctx); err != nil {
			return "", err
		}
	}

	blobName := safeName
	if d.prefix != "" {
		blobName = path.Join(d.prefix, safeName)
	}

	f, err := os.Open(localTempPath)
	if err != nil {
		return "", fmt.Errorf("open temp archive for Azure upload: %w", err)
	}
	defer f.Close()

	contentType := "application/gzip"
	sourceTag := "true"
	opts := &azblob.UploadFileOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &contentType,
		},
		Metadata: map[string]*string{
			"dockvault": &sourceTag,
			"filename":  &safeName,
		},
	}
	if d.accessTier != "" {
		tier := d.accessTier
		opts.AccessTier = &tier
	}

	if _, err := d.client.UploadFile(ctx, d.container, blobName, f, opts); err != nil {
		return "", fmt.Errorf("upload backup to azure://%s/%s: %w", d.container, blobName, err)
	}

	_ = os.Remove(localTempPath)
	return blobName, nil
}

func (d *AzureDriver) Open(ctx context.Context, storagePath string) (io.ReadCloser, int64, error) {
	blobName := strings.TrimPrefix(strings.TrimSpace(storagePath), "/")
	if blobName == "" {
		return nil, 0, fmt.Errorf("empty Azure blob path")
	}

	resp, err := d.client.DownloadStream(ctx, d.container, blobName, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobArchived) {
			return nil, 0, fmt.Errorf("%w: %s", ErrAzureArchiveTier, blobName)
		}
		return nil, 0, fmt.Errorf("download Azure blob %q: %w", blobName, err)
	}

	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}
	return resp.Body, size, nil
}

func (d *AzureDriver) Delete(ctx context.Context, storagePath string) error {
	blobName := strings.TrimPrefix(strings.TrimSpace(storagePath), "/")
	if blobName == "" {
		return nil
	}
	if _, err := d.client.DeleteBlob(ctx, d.container, blobName, nil); err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil
		}
		return fmt.Errorf("delete Azure blob %q: %w", blobName, err)
	}
	return nil
}

// List enumerates backup archives in the configured Azure Blob container and prefix.
func (d *AzureDriver) List(ctx context.Context) ([]RemoteObject, error) {
	var prefixPtr *string
	if d.prefix != "" {
		p := d.prefix + "/"
		prefixPtr = &p
	}

	pager := d.client.NewListBlobsFlatPager(d.container, &azblob.ListBlobsFlatOptions{
		Prefix: prefixPtr,
	})

	var out []RemoteObject
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list Azure blobs: %w", err)
		}
		if page.Segment == nil {
			continue
		}
		for _, item := range page.Segment.BlobItems {
			if item.Name == nil {
				continue
			}
			blobPath := *item.Name
			name := path.Base(blobPath)
			if !IsBackupArchiveName(name) {
				continue
			}
			var size int64
			var modTime time.Time
			if item.Properties != nil {
				if item.Properties.ContentLength != nil {
					size = *item.Properties.ContentLength
				}
				if item.Properties.LastModified != nil {
					modTime = item.Properties.LastModified.UTC()
				}
			}
			out = append(out, RemoteObject{
				Name:         name,
				StoragePath:  blobPath,
				SizeBytes:    size,
				LastModified: modTime,
				Encrypted:    strings.HasSuffix(name, ".enc"),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].LastModified.After(out[j].LastModified)
	})
	return out, nil
}

// GenerateSASURL creates a read-only time-limited SAS URL for direct blob download.
func (d *AzureDriver) GenerateSASURL(ctx context.Context, storagePath string, ttl time.Duration) (string, error) {
	if !d.canGenerateSAS {
		return "", fmt.Errorf("SAS link generation requires Account Key, Connection String, or Service Principal authentication")
	}
	blobName := strings.TrimPrefix(strings.TrimSpace(storagePath), "/")
	if blobName == "" {
		return "", fmt.Errorf("empty Azure blob path")
	}

	now := time.Now().UTC()
	start := now.Add(-5 * time.Minute)
	expiry := now.Add(ttl)
	sigValues := sas.BlobSignatureValues{
		Version:       "2024-11-04",
		ContainerName: d.container,
		BlobName:      blobName,
		Permissions:   (&sas.BlobPermissions{Read: true}).String(),
		StartTime:     start,
		ExpiryTime:    expiry,
	}

	var (
		qp  sas.QueryParameters
		err error
	)
	if d.useUserDelegationSAS {
		startStr := start.Format(sas.TimeFormat)
		expiryStr := expiry.Format(sas.TimeFormat)
		udc, udcErr := d.client.ServiceClient().GetUserDelegationCredential(ctx, service.KeyInfo{
			Start:  &startStr,
			Expiry: &expiryStr,
		}, nil)
		if udcErr != nil {
			return "", fmt.Errorf("request Azure User Delegation Key (requires Storage Blob Delegator role): %w", udcErr)
		}
		qp, err = sigValues.SignWithUserDelegation(udc)
	} else {
		if d.sharedKeyCred == nil {
			return "", fmt.Errorf("shared key credential unavailable for SAS signing")
		}
		qp, err = sigValues.SignWithSharedKey(d.sharedKeyCred)
	}
	if err != nil {
		return "", fmt.Errorf("generate SAS URL for %q: %w", blobName, err)
	}
	return fmt.Sprintf("%s/%s/%s?%s", d.serviceURL, d.container, blobName, qp.Encode()), nil
}

func (d *AzureDriver) ensureContainer(ctx context.Context) (bool, error) {
	containerClient := d.client.ServiceClient().NewContainerClient(d.container)
	_, err := containerClient.GetProperties(ctx, nil)
	if err == nil {
		return false, nil
	}

	if bloberror.HasCode(err, bloberror.ContainerNotFound) {
		if !d.autoCreateContainer {
			return false, fmt.Errorf("Azure container %q does not exist (enable auto-create or create it first)", d.container)
		}
		if _, createErr := d.client.CreateContainer(ctx, d.container, nil); createErr != nil {
			if bloberror.HasCode(createErr, bloberror.ContainerAlreadyExists) {
				return false, nil
			}
			return false, fmt.Errorf("create Azure container %q: %w", d.container, createErr)
		}
		return true, nil
	}

	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		return false, fmt.Errorf("check Azure container %q (%s): %w", d.container, respErr.ErrorCode, err)
	}
	return false, fmt.Errorf("check Azure container %q: %w", d.container, err)
}

func resolveAzureServiceURL(accountName, endpoint string) string {
	if endpoint != "" {
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			endpoint = "https://" + endpoint
		}
		return strings.TrimRight(endpoint, "/")
	}
	return fmt.Sprintf("https://%s.blob.core.windows.net", accountName)
}

func buildAzureSASServiceURL(accountName, endpoint, rawSAS string) (string, error) {
	sasStr := strings.TrimSpace(rawSAS)
	// Allow users to paste either a full URL with ?sv=... or just the SAS query token.
	if strings.HasPrefix(sasStr, "http://") || strings.HasPrefix(sasStr, "https://") {
		return sasStr, nil
	}
	if accountName == "" && endpoint == "" {
		return "", fmt.Errorf("Azure account name or custom endpoint is required with a SAS token")
	}
	base := resolveAzureServiceURL(accountName, endpoint)
	token := strings.TrimPrefix(sasStr, "?")
	return base + "?" + token, nil
}

func parseAzureConnectionString(connStr string) (serviceURL, accountName, accountKey string, ok bool) {
	parts := strings.Split(connStr, ";")
	kv := make(map[string]string, len(parts))
	for _, part := range parts {
		idx := strings.Index(part, "=")
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(part[:idx])
		v := strings.TrimSpace(part[idx+1:])
		kv[k] = v
	}

	// Azurite shorthand connection string uses a fixed dev account; let azblob handle it natively.
	if strings.EqualFold(kv["UseDevelopmentStorage"], "true") {
		return "", "", "", false
	}

	accountName = kv["AccountName"]
	accountKey = kv["AccountKey"]
	if accountName == "" || accountKey == "" {
		return "", "", "", false
	}

	if blobEndpoint := kv["BlobEndpoint"]; blobEndpoint != "" {
		return strings.TrimRight(blobEndpoint, "/"), accountName, accountKey, true
	}

	proto := kv["DefaultEndpointsProtocol"]
	if proto == "" {
		proto = "https"
	}
	suffix := kv["EndpointSuffix"]
	if suffix == "" {
		suffix = "core.windows.net"
	}
	return fmt.Sprintf("%s://%s.blob.%s", proto, accountName, suffix), accountName, accountKey, true
}

func parseAzureAccessTier(raw string) (blob.AccessTier, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return blob.AccessTierCool, nil
	}
	switch strings.ToLower(t) {
	case "hot":
		return blob.AccessTierHot, nil
	case "cool":
		return blob.AccessTierCool, nil
	case "cold":
		return blob.AccessTierCold, nil
	case "archive":
		return blob.AccessTierArchive, nil
	default:
		return "", fmt.Errorf("invalid Azure access tier %q (expected Hot, Cool, Cold, or Archive)", raw)
	}
}
