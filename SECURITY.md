# Security Policy

## Supported Versions

We take the security of `k8s-rightsizer` seriously. Currently, we provide security updates for the following versions:

| Version       | Supported          |
| ------------- | ------------------ |
| active        | :white_check_mark: |
| < 0.2.0-alpha | :x:                |

## Our Commitment

`k8s-rightsizer` operates within your Kubernetes cluster with permissions to modify workload resources. Because of this, we follow these principles:
* **Minimal RBAC**: We always recommend the minimum set of permissions (least privilege) required for the tool to function.
* **No Data Export**: The tool does not send your cluster data or metrics to any external server. Logs are sent on standard output.
* **Scanning**: Container images are scanned for known vulnerabilities (CVEs) during the CI/CD process.

## Reporting a Vulnerability

**Please do not open public GitHub issues for security vulnerabilities.**

If you believe you have found a security vulnerability in this project, please report it responsibly by following these steps:

1. **Direct Contact**: Send an email to **security@k8srightsizer.com**
2. **Details**: Include a detailed description of the vulnerability, steps to reproduce it, and the potential impact.
3. **Response**: You will receive an acknowledgment of your report within 48 hours.
4. **Resolution**: We will work with you to understand the scope and fix the issue before making any public disclosure.

## Security Best Practices for Users

To keep your cluster secure while using `k8s-rightsizer`, we recommend:
1. **Namespace Isolation**: Run the Job in a dedicated namespace with restricted access.
2. **Secrets Management**: If you are using sensitive data, provide them via Kubernetes Secrets or a Vault provider, never hardcoded in the Job manifest.
3. **Read-Only First**: Always run with the `--dry-run` flag first to audit the changes the tool intends to make.

Thank you for helping keep `k8s-rightsizer` and the Kubernetes community safe!
