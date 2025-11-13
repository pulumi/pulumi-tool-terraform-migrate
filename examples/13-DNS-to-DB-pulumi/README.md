# DNS to DB - Pulumi TypeScript Translation

This project is a Pulumi TypeScript translation of the Terraform configuration from `../terraform-manifests`. It provisions a complete AWS infrastructure including VPC, EC2 instances, RDS MySQL database, Application Load Balancer with SSL/TLS, and Route53 DNS configuration.

## Architecture Overview

This infrastructure creates:
- **VPC** with public, private, and database subnets across 2 availability zones
- **NAT Gateways** for private subnet internet access
- **Security Groups** for bastion host, private instances, load balancer, and RDS
- **EC2 Instances**:
  - 1 Bastion host in public subnet
  - 2 Application instances (app1) in private subnets
- **RDS MySQL Database** (multi-AZ) with parameter and option groups
- **Application Load Balancer** with:
  - HTTP to HTTPS redirect
  - SSL/TLS certificate from ACM
  - 3 target groups with path-based routing
  - Health checks configured
- **Route53 DNS** record pointing to the ALB
- **ACM Certificate** for `pulumi-demos.net` and `*.pulumi-demos.net`

## Prerequisites

- Pulumi CLI (>= v3.0): https://www.pulumi.com/docs/get-started/install/
- Node.js (>= 18): https://nodejs.org/
- AWS credentials configured (e.g., via `aws configure` or environment variables)
- An existing Route53 hosted zone for `pulumi-demos.net`
- An existing EC2 key pair named `terraform-key` (or configure a different one)

## Translation Notes

This Pulumi program translates the following Terraform modules to native Pulumi resources:
- `terraform-aws-modules/vpc/aws` → Native VPC, Subnet, RouteTable, NatGateway, etc.
- `terraform-aws-modules/security-group/aws` → Native SecurityGroup resources
- `terraform-aws-modules/ec2-instance/aws` → Native EC2 Instance resources
- `terraform-aws-modules/rds/aws` → Native RDS Instance with supporting resources
- `terraform-aws-modules/alb/aws` → Native ALB, TargetGroup, Listener resources
- `terraform-aws-modules/acm/aws` → Native ACM Certificate with validation

Key differences from Terraform:
1. **No modules**: All resources are created directly using `@pulumi/aws` resources
2. **Type safety**: TypeScript provides compile-time type checking
3. **Async/await**: Pulumi uses promises for resource outputs
4. **Configuration**: Uses Pulumi's configuration system instead of Terraform variables
5. **Secrets**: Database passwords use Pulumi's secret management

## Configuration

The following configuration values are required or have defaults:

| Configuration Key | Description | Default | Required |
|-------------------|-------------|---------|----------|
| `aws:region` | AWS region | `us-east-1` | No |
| `environment` | Environment name | `dev` | No |
| `businessDivision` | Business division | `sap` | No |
| `vpcName` | VPC name | `myvpc` | No |
| `vpcCidrBlock` | VPC CIDR block | `10.0.0.0/16` | No |
| `instanceType` | EC2 instance type | `t3.micro` | No |
| `instanceKeypair` | EC2 key pair name | - | **Yes** |
| `dbName` | RDS database name | - | **Yes** |
| `dbInstanceIdentifier` | RDS instance identifier | - | **Yes** |
| `dbUsername` | RDS admin username | - | **Yes** |
| `dbPassword` | RDS admin password (secret) | - | **Yes** |

## Getting Started

1. **Install dependencies:**

   ```bash
   npm install
   ```

2. **Configure required settings:**

   ```bash
   pulumi config set instanceKeypair your-key-pair-name
   pulumi config set dbName webappdb
   pulumi config set dbInstanceIdentifier webappdb
   pulumi config set dbUsername dbadmin
   pulumi config set --secret dbPassword YourSecurePassword123!
   ```

3. **Preview the infrastructure:**

   ```bash
   pulumi preview
   ```

4. **Deploy the infrastructure:**

   ```bash
   pulumi up
   ```

   This will create approximately 50+ AWS resources. The deployment takes about 15-20 minutes, primarily due to:
   - RDS instance creation (~10 minutes)
   - ACM certificate validation (~5 minutes)
   - NAT Gateway creation (~2-3 minutes)

5. **View outputs:**

   ```bash
   pulumi stack output
   ```

   Key outputs include:
   - `applicationUrl`: The HTTPS URL to access the application
   - `albDnsName`: ALB DNS name
   - `dbInstanceEndpoint`: RDS connection endpoint
   - `bastionPublicIp`: Bastion host public IP for SSH access

## Project Structure

```
pulumi-translation/
├── index.ts              # Main Pulumi program (all infrastructure)
├── package.json          # Node.js dependencies
├── Pulumi.yaml          # Pulumi project metadata
├── Pulumi.dev.yaml      # Stack configuration (dev environment)
├── tsconfig.json        # TypeScript configuration
└── README.md            # This file
```

## Terraform Comparison

### Terraform Structure (30 files)
- `c1-versions.tf` - Provider configuration
- `c2-generic-variables.tf` - Generic variables
- `c3-local-values.tf` - Local values
- `c4-*-vpc-*.tf` - VPC module and configuration (3 files)
- `c5-*-securitygroup-*.tf` - Security group modules (5 files)
- `c6-*-datasource-*.tf` - Data sources (2 files)
- `c7-*-ec2instance-*.tf` - EC2 instances (5 files)
- `c8-elasticip.tf` - Elastic IPs
- `c9-nullresource-provisioners.tf` - Null resources
- `c10-*-ALB-*.tf` - Application Load Balancer (3 files)
- `c11-acm-certificatemanager.tf` - ACM certificate
- `c12-route53-dnsregistration.tf` - Route53 DNS
- `c13-*-rdsdb-*.tf` - RDS database (3 files)

### Pulumi Structure (1 file)
- `index.ts` - All infrastructure in a single, well-organized file (~660 lines)

The Pulumi translation consolidates 30 Terraform files into one TypeScript file while maintaining clarity through:
- Comments indicating the original Terraform file
- Logical grouping of related resources
- Clear section headers

## Cleanup

To destroy all resources:

```bash
pulumi destroy
```

To remove the stack completely:

```bash
pulumi stack rm dev
```

## Notes

1. **User Data Scripts**: The EC2 instances reference shell scripts from `../terraform-manifests/` directory:
   - `jumpbox-install.sh` - Bastion host setup
   - `app1-install.sh` - Application 1 setup

2. **Route53 Zone**: You must have a pre-existing Route53 hosted zone for `pulumi-demos.net`

3. **Cost Considerations**: This infrastructure creates resources that incur costs:
   - NAT Gateway (~$32/month)
   - RDS db.t3.small (~$25/month)
   - EC2 t3.micro instances (~$7/month each)
   - Application Load Balancer (~$16/month)

4. **Security**: The bastion host security group allows SSH from anywhere (0.0.0.0/0). Consider restricting this to your IP address for production use.

5. **Incomplete Translation**: This translation includes only the App1 instances. The original Terraform has App2 and App3 instances which would need to be added for full parity.

## Additional Resources

- [Pulumi AWS Documentation](https://www.pulumi.com/docs/reference/pkg/aws/)
- [Pulumi TypeScript Guide](https://www.pulumi.com/docs/intro/languages/javascript/)
- [AWS VPC Documentation](https://docs.aws.amazon.com/vpc/)
- [AWS ALB Documentation](https://docs.aws.amazon.com/elasticloadbalancing/)
- [AWS RDS Documentation](https://docs.aws.amazon.com/rds/)
