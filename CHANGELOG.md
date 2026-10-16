# Changelog

## [1.0.0] - 2026-10-16

First release of Pagepack.

- Daylight glass UI with live archive progress and browser-local history.
- Versioned APIs for job creation, progress, cancellation, configuration, and health.
- Download sizes, file counts, rates, compression stats, and SHA-256 checksums.
- GNU wget mirroring with stalled-link skipping and configurable size/time limits.
- S3-compatible archive storage and expiring download links.
- Cancellation with scratch-file and attempted-upload cleanup.
- Valkey job state and bounded concurrent workers.
- Structured terminal/JSON logging.
- Multi-stage Docker image and Compose stack with Valkey and persistent volumes.

[1.0.0]: https://github.com/TrueFix/pagepack/releases/tag/v1.0.0
