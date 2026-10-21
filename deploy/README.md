# Production deploys

Merges to `main` auto-deploy via `.github/workflows/deploy.yml` (test → `sam build` → `sam deploy`).
Auth uses GitHub OIDC, so there are no long-lived AWS keys. One-time setup below.

## One-time AWS setup

Run these from a machine with admin AWS creds. There is nothing to create for
artifact storage: every `sam deploy` stages its packaged CloudFormation
template in S3, and `resolve_s3 = true` in `samconfig.toml` makes SAM use its
auto-created bucket (`aws-sam-cli-managed-default-samclisourcebucket-dvijzdyhur0e`).
The Lambda code itself ships as a container image to ECR, not S3.

1. GitHub OIDC provider (skip if it already exists):

```bash
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1
```

2. Deploy role (trust is scoped to `refs/heads/main` of this repo only):

```bash
aws iam create-role --role-name voting-api-github-deploy \
  --assume-role-policy-document file://deploy/role-trust-policy.json \
  --description "GitHub Actions deploy role for voting-api (main only)"
aws iam put-role-policy --role-name voting-api-github-deploy \
  --policy-name sam-deploy \
  --policy-document file://deploy/role-permissions-policy.json
```

## What the role can do

`deploy/role-permissions-policy.json` is scoped to this stack only:

- CloudFormation full access on `stack/voting-api/*` (plus read-only
  stack listing, which `sam` needs and can't be resource-scoped)
- S3 data ops on the SAM-managed staging bucket above (SAM uploads the
  packaged template there on every deploy, even for image-based Lambdas)
- ECR image push on the existing `votingapiec26d099/icaavoting20834c33repo` repo
  (`ecr:GetAuthorizationToken` on `*` is required by AWS, it can't be scoped)
- Lambda `*` on `function:voting-api-*` (SAM names functions
  `<stack>-<resource>-<hash>`)
- `apigatewayv2:*` (API Gateway has no resource-level scoping granular enough
  for SAM's HttpApi + mapping management to be practical)
- IAM role management + `PassRole` on `role/voting-api-*` (SAM creates
  the Lambda execution role; `CAPABILITY_IAM` is already in `samconfig.toml`)
- CloudWatch Logs on `/aws/lambda/voting-api-*`

No DynamoDB, SSM, or secret access: deploys only ship code, runtime config
comes from SSM at Lambda startup via the execution role in `template.yml`.
