package provider

import "context"

type Provider interface {
	Test(ctx context.Context) error
	EnsureDir(ctx context.Context, remoteDir string) error
	Upload(ctx context.Context, localPath, remotePath string) error
}
