package storage

import (
	"context"
	"mime"
	"os"
	"time"

	appconfig "github.com/TrueFix/pagepack/pkg/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3 struct {
	client *s3.Client
	bucket string
	ttl    time.Duration
}

func New(ctx context.Context, c appconfig.Config) (*S3, error) {
	options := []func(*config.LoadOptions) error{config.WithRegion(c.S3Region)}
	if c.AWSAccessKeyID != "" {
		options = append(options, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AWSAccessKeyID, c.AWSSecretAccessKey, c.AWSSessionToken)))
	}
	awsConfig, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		o.UsePathStyle = c.S3ForcePathStyle
		if c.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(c.S3Endpoint)
		}
	})
	return &S3{client: client, bucket: c.S3Bucket, ttl: c.DownloadTTL}, nil
}

// UploadOptions describe how an archive is stored and reported.
type UploadOptions struct {
	// DownloadName is the filename browsers save the object as.
	DownloadName string
	// OnProgress receives the number of bytes sent so far and the total size.
	OnProgress func(sent, total int64)
}

func (s *S3) Upload(ctx context.Context, key, filename string, o UploadOptions) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	name := o.DownloadName
	if name == "" {
		name = "website.zip"
	}
	body := &progressFile{file: f, total: info.Size(), report: o.OnProgress}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: body, ContentLength: aws.Int64(info.Size()),
		ContentType: aws.String("application/zip"), ContentDisposition: aws.String(mime.FormatMediaType("attachment", map[string]string{"filename": name})),
	})
	return err
}

// DownloadURL presigns a GET link and reports when it expires.
func (s *S3) DownloadURL(ctx context.Context, key string) (string, time.Time, error) {
	expires := time.Now().Add(s.ttl)
	res, err := s3.NewPresignClient(s.client).PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, func(o *s3.PresignOptions) { o.Expires = s.ttl })
	if err != nil {
		return "", time.Time{}, err
	}
	return res.URL, expires, nil
}

// progressFile counts bytes as the SDK reads the body. The SDK may rewind (Seek) to
// hash or retry, so the position—not a running sum—is what gets reported.
// Only Read and Seek are exposed: embedding *os.File would leak WriteTo and let the
// transport bypass counting.
type progressFile struct {
	file   *os.File
	total  int64
	pos    int64
	report func(sent, total int64)
}

func (p *progressFile) Read(b []byte) (int, error) {
	n, err := p.file.Read(b)
	p.pos += int64(n)
	if p.report != nil && n > 0 {
		p.report(p.pos, p.total)
	}
	return n, err
}

func (p *progressFile) Seek(offset int64, whence int) (int64, error) {
	pos, err := p.file.Seek(offset, whence)
	if err == nil {
		p.pos = pos
	}
	return pos, err
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}
