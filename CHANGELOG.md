# Changelog

## [0.3.0] - 2026-06-19

### Bug Fixes

- CVEs (#22)
- **rightsizer**: remove x86 bias
- wait for file  recommendations to avoid backoff
- correct context usage in post-rollout soak verification

### Features

- recommendation process indicator (#27)
- enhance error checking to ignore terminating and stale OOMKilled
- **rightsizer**: add post rollout stability check
- **rightsizer**: node compatibility recheck
- reduce delay in processing and add post-rollout stability check
- optimize Excel row reading and enhance numeric data normalization
- improve log messages across the application
- enhance node compatibility checks
- **rightsizer**: add post-rollout check functionality
- add configurable inter-recommendation delay
- add resize event statuses tracking
- add node compatibility check configuration
- update default resize strategy to workload

### Refactoring

- simplify NodeCheck logic and improve error handling
- change log level for detailed processing messages
- improve resource cleanup and fix time comparison logic


