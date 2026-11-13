// Package uploader transfers entries over SFTP through per-profile FIFO
// queues: remote path resolution, directory creation, atomic temporary-file
// writes, conflict policies, progress, retries, and cancellation.
package uploader
