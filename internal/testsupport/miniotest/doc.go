// Package miniotest is the integration tests' S3-compatible store: a
// throwaway MinIO in a container, and the lake the #118 end-to-end tests read
// from it. Its code is built only with the integration tag; nothing in a
// normal build imports it.
package miniotest
