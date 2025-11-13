import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";
import * as awsx from "@pulumi/awsx";
import * as fs from "fs";

// Configuration
const config = new pulumi.Config();
const awsConfig = new pulumi.Config("aws");

// Generic Variables
const awsRegion = awsConfig.get("region") || "us-east-1";
const environment = config.get("environment") || "dev";
const businessDivision = config.get("businessDivision") || "sap";

// VPC Variables
const vpcName = config.get("vpcName") || "myvpc";
const vpcCidrBlock = config.get("vpcCidrBlock") || "10.0.0.0/16";
const vpcAvailabilityZones = config.getObject<string[]>("vpcAvailabilityZones") || ["us-east-1a", "us-east-1b"];
const vpcPublicSubnets = config.getObject<string[]>("vpcPublicSubnets") || ["10.0.101.0/24", "10.0.102.0/24"];
const vpcPrivateSubnets = config.getObject<string[]>("vpcPrivateSubnets") || ["10.0.1.0/24", "10.0.2.0/24"];
const vpcDatabaseSubnets = config.getObject<string[]>("vpcDatabaseSubnets") || ["10.0.151.0/24", "10.0.152.0/24"];

// EC2 Variables
const instanceType = config.get("instanceType") || "t3.micro";
const instanceKeypair = config.get("instanceKeypair") || "anton-20251005-terraform-key";

// RDS Variables
const dbName = config.require("dbName");
const dbInstanceIdentifier = config.require("dbInstanceIdentifier");
const dbUsername = config.requireSecret("dbUsername");
const dbPassword = config.requireSecret("dbPassword");

// Local Values
const owners = businessDivision;
const envName = environment;
const name = `${businessDivision}-${environment}`;
const commonTags = {
    owners: owners,
    environment: envName,
    Owner: "anton",
};

// Data Sources
// Get latest Amazon Linux 2 AMI
const amzlinux2 = aws.ec2.getAmi({
    mostRecent: true,
    owners: ["amazon"],
    filters: [
        { name: "name", values: ["amzn2-ami-hvm-*-gp2"] },
        { name: "root-device-type", values: ["ebs"] },
        { name: "virtualization-type", values: ["hvm"] },
        { name: "architecture", values: ["x86_64"] },
    ],
});

// Get Route53 Zone
const mydomain = aws.route53.getZone({
    name: "pulumi-demos.net",
});

// VPC
const vpc = new awsx.ec2.Vpc(`${name}-${vpcName}`, {
    cidrBlock: vpcCidrBlock,
    numberOfAvailabilityZones: vpcAvailabilityZones.length,
    subnetSpecs: [
        {
            type: awsx.ec2.SubnetType.Public,
            cidrMask: 24,
        },
        {
            type: awsx.ec2.SubnetType.Private,
            cidrMask: 24,
        },
        {
            type: awsx.ec2.SubnetType.Isolated,
            cidrMask: 24,
            name: "database",
        },
    ],
    natGateways: {
        strategy: awsx.ec2.NatGatewayStrategy.Single,
    },
    enableDnsHostnames: true,
    enableDnsSupport: true,
    tags: {
        ...commonTags,
        Name: `${name}-${vpcName}`,
    },
}, {
    transformations: [(args) => {
        if (args.type === "aws:ec2/subnet:Subnet") {
            const subnetName = args.name;
            if (subnetName.includes("public")) {
                return {
                    props: {
                        ...args.props,
                        tags: { ...args.props.tags, Type: "Public Subnets" },
                    },
                    opts: args.opts,
                };
            } else if (subnetName.includes("private")) {
                return {
                    props: {
                        ...args.props,
                        tags: { ...args.props.tags, Type: "Private Subnets" },
                    },
                    opts: args.opts,
                };
            } else if (subnetName.includes("database")) {
                return {
                    props: {
                        ...args.props,
                        tags: { ...args.props.tags, Type: "Private Database Subnets" },
                    },
                    opts: args.opts,
                };
            }
        }
        return undefined;
    }],
});

// DB Subnet Group
const dbSubnetGroup = new aws.rds.SubnetGroup("rds-subnet-group", {
    subnetIds: vpc.isolatedSubnetIds,
    tags: {
        ...commonTags,
        Name: `${name}-rds-subnet-group`,
    },
});

// Security Groups
// Bastion Host Security Group
const publicBastionSg = new aws.ec2.SecurityGroup("public-bastion-sg", {
    name: "public-bastion-sg",
    description: "Security group for bastion host",
    vpcId: vpc.vpcId,
    ingress: [
        {
            protocol: "tcp",
            fromPort: 22,
            toPort: 22,
            cidrBlocks: ["0.0.0.0/0"],
            description: "SSH from anywhere",
        },
    ],
    egress: [
        {
            protocol: "-1",
            fromPort: 0,
            toPort: 0,
            cidrBlocks: ["0.0.0.0/0"],
            description: "Allow all outbound",
        },
    ],
    tags: {
        ...commonTags,
        Name: "public-bastion-sg",
    },
});

// Private EC2 Security Group
const privateSg = new aws.ec2.SecurityGroup("private-sg", {
    name: "private-sg",
    description: "Security group for private instances",
    vpcId: vpc.vpcId,
    ingress: [
        {
            protocol: "tcp",
            fromPort: 22,
            toPort: 22,
            cidrBlocks: [vpcCidrBlock],
            description: "SSH from VPC",
        },
        {
            protocol: "tcp",
            fromPort: 80,
            toPort: 80,
            cidrBlocks: [vpcCidrBlock],
            description: "HTTP from VPC",
        },
        {
            protocol: "tcp",
            fromPort: 8080,
            toPort: 8080,
            cidrBlocks: [vpcCidrBlock],
            description: "HTTP 8080 from VPC",
        },
    ],
    egress: [
        {
            protocol: "-1",
            fromPort: 0,
            toPort: 0,
            cidrBlocks: ["0.0.0.0/0"],
            description: "Allow all outbound",
        },
    ],
    tags: {
        ...commonTags,
        Name: "private-sg",
    },
});

// Load Balancer Security Group
const loadbalancerSg = new aws.ec2.SecurityGroup("loadbalancer-sg", {
    name: "loadbalancer-sg",
    description: "Security group for load balancer",
    vpcId: vpc.vpcId,
    ingress: [
        {
            protocol: "tcp",
            fromPort: 80,
            toPort: 80,
            cidrBlocks: ["0.0.0.0/0"],
            description: "HTTP from anywhere",
        },
        {
            protocol: "tcp",
            fromPort: 443,
            toPort: 443,
            cidrBlocks: ["0.0.0.0/0"],
            description: "HTTPS from anywhere",
        },
        {
            protocol: "tcp",
            fromPort: 81,
            toPort: 81,
            cidrBlocks: ["0.0.0.0/0"],
            description: "Port 81 from anywhere",
        },
    ],
    egress: [
        {
            protocol: "-1",
            fromPort: 0,
            toPort: 0,
            cidrBlocks: ["0.0.0.0/0"],
            description: "Allow all outbound",
        },
    ],
    tags: {
        ...commonTags,
        Name: "loadbalancer-sg",
    },
});

// RDS Database Security Group
const rdsdbSg = new aws.ec2.SecurityGroup("rdsdb-sg", {
    name: "rdsdb-sg",
    description: "Security group for RDS database",
    vpcId: vpc.vpcId,
    ingress: [
        {
            protocol: "tcp",
            fromPort: 3306,
            toPort: 3306,
            cidrBlocks: [vpcCidrBlock],
            description: "MySQL from VPC",
        },
    ],
    egress: [
        {
            protocol: "-1",
            fromPort: 0,
            toPort: 0,
            cidrBlocks: ["0.0.0.0/0"],
            description: "Allow all outbound",
        },
    ],
    tags: {
        ...commonTags,
        Name: "rdsdb-sg",
    },
});

// RDS Parameter Group
const rdsParameterGroup = new aws.rds.ParameterGroup("rds-parameter-group", {
    family: "mysql8.0",
    parameters: [
        {
            name: "character_set_client",
            value: "utf8mb4",
        },
        {
            name: "character_set_server",
            value: "utf8mb4",
        },
    ],
    tags: {
        ...commonTags,
        Name: `${name}-mysql8-parameter-group`,
    },
});

// RDS Database
const rdsdb = new aws.rds.Instance("rdsdb", {
    identifier: dbInstanceIdentifier,
    engine: "mysql",
    engineVersion: "8.0.40",
    instanceClass: "db.t3.small",
    allocatedStorage: 20,
    maxAllocatedStorage: 100,
    storageType: "gp2",
    storageEncrypted: false,
    dbName: dbName,
    username: dbUsername,
    password: dbPassword,
    port: 3306,
    multiAz: true,
    dbSubnetGroupName: dbSubnetGroup.name,
    vpcSecurityGroupIds: [rdsdbSg.id],
    parameterGroupName: rdsParameterGroup.name,
    backupRetentionPeriod: 0,
    skipFinalSnapshot: true,
    deletionProtection: false,
    enabledCloudwatchLogsExports: ["general"],
    maintenanceWindow: "Mon:00:00-Mon:03:00",
    backupWindow: "03:00-06:00",
    applyImmediately: true,
    tags: {
        ...commonTags,
        Name: dbInstanceIdentifier,
    },
});

// User Data Scripts
const jumpboxUserData = `#!/bin/bash
sudo yum update -y
sudo rpm -e --nodeps mariadb-libs-*
sudo amazon-linux-extras enable mariadb10.5
sudo yum clean metadata
sudo yum install -y mariadb
sudo mysql -V
sudo yum install -y telnet
`;

const app1UserData = `#!/bin/bash
sudo yum update -y
sudo yum install -y httpd
sudo systemctl enable httpd
sudo service httpd start
sudo echo '<h1>Welcome to StackSimplify - APP-1</h1>' | sudo tee /var/www/html/index.html
sudo mkdir /var/www/html/app1
sudo echo '<!DOCTYPE html> <html> <body style="background-color:rgb(250, 210, 210);"> <h1>Welcome to Stack Simplify - APP-1</h1> <p>Terraform Demo</p> <p>Application Version: V1</p> </body></html>' | sudo tee /var/www/html/app1/index.html
TOKEN=\`curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600"\`
sudo curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/dynamic/instance-identity/document -o /var/www/html/app1/metadata.html
`;

const app2UserData = `#!/bin/bash
sudo yum update -y
sudo yum install -y httpd
sudo systemctl enable httpd
sudo service httpd start
sudo echo '<h1>Welcome to StackSimplify - APP-2</h1>' | sudo tee /var/www/html/index.html
sudo mkdir /var/www/html/app2
sudo echo '<!DOCTYPE html> <html> <body style="background-color:rgb(15, 232, 192);"> <h1>Welcome to Stack Simplify - APP-2</h1> <p>Terraform Demo</p> <p>Application Version: V1</p> </body></html>' | sudo tee /var/www/html/app2/index.html
TOKEN=\`curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600"\`
sudo curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/dynamic/instance-identity/document -o /var/www/html/app1/metadata.html
`;

// EC2 Instances
// Bastion Host
const bastionHost = new aws.ec2.Instance("bastion-host", {
    ami: amzlinux2.then(ami => ami.id),
    instanceType: instanceType,
    keyName: instanceKeypair,
    subnetId: vpc.publicSubnetIds[0],
    vpcSecurityGroupIds: [publicBastionSg.id],
    userData: jumpboxUserData,
    tags: {
        ...commonTags,
        Name: `${environment}-BastionHost`,
    },
});

// Elastic IP for Bastion Host
const bastionEip = new aws.ec2.Eip("bastion-eip", {
    instance: bastionHost.id,
    domain: "vpc",
    tags: {
        ...commonTags,
        Name: `${environment}-bastion-eip`,
    },
});

// App1 Private Instances
const app1Instances: aws.ec2.Instance[] = [];
for (let i = 0; i < 2; i++) {
    const instance = new aws.ec2.Instance(`app1-instance-${i}`, {
        ami: amzlinux2.then(ami => ami.id),
        instanceType: instanceType,
        keyName: instanceKeypair,
        subnetId: vpc.privateSubnetIds.apply(subnets => subnets[i % subnets.length]),
        vpcSecurityGroupIds: [privateSg.id],
        userData: app1UserData,
        tags: {
            ...commonTags,
            Name: `${environment}-app1`,
        },
    }, { dependsOn: [vpc] });
    app1Instances.push(instance);
}

// App2 Private Instances
const app2Instances: aws.ec2.Instance[] = [];
for (let i = 0; i < 2; i++) {
    const instance = new aws.ec2.Instance(`app2-instance-${i}`, {
        ami: amzlinux2.then(ami => ami.id),
        instanceType: instanceType,
        keyName: instanceKeypair,
        subnetId: vpc.privateSubnetIds.apply(subnets => subnets[i % subnets.length]),
        vpcSecurityGroupIds: [privateSg.id],
        userData: app2UserData,
        tags: {
            ...commonTags,
            Name: `${environment}-app2`,
        },
    }, { dependsOn: [vpc] });
    app2Instances.push(instance);
}

// App3 Private Instances (with RDS connection)
const app3Instances: aws.ec2.Instance[] = [];
for (let i = 0; i < 2; i++) {
    const app3UserData = pulumi.all([rdsdb.endpoint, dbUsername, dbPassword]).apply(([endpoint, username, password]) => `#!/bin/bash
sudo amazon-linux-extras enable java-openjdk11
sudo yum clean metadata && sudo yum -y install java-11-openjdk
mkdir /home/ec2-user/app3-usermgmt && cd /home/ec2-user/app3-usermgmt
wget https://github.com/stacksimplify/temp1/releases/download/1.0.0/usermgmt-webapp.war -P /home/ec2-user/app3-usermgmt
export DB_HOSTNAME=${endpoint}
export DB_PORT=3306
export DB_NAME=${dbName}
export DB_USERNAME=${username}
export DB_PASSWORD=${password}
java -jar /home/ec2-user/app3-usermgmt/usermgmt-webapp.war > /home/ec2-user/app3-usermgmt/ums-start.log &
`);

    const instance = new aws.ec2.Instance(`app3-instance-${i}`, {
        ami: amzlinux2.then(ami => ami.id),
        instanceType: instanceType,
        keyName: instanceKeypair,
        subnetId: vpc.privateSubnetIds.apply(subnets => subnets[i % subnets.length]),
        vpcSecurityGroupIds: [privateSg.id],
        userData: app3UserData,
        tags: {
            ...commonTags,
            Name: `${environment}-app3`,
        },
    }, { dependsOn: [vpc, rdsdb] });
    app3Instances.push(instance);
}

// ACM Certificate
const certificate = new aws.acm.Certificate("cert", {
    domainName: "pulumi-demos.net",
    subjectAlternativeNames: ["*.pulumi-demos.net"],
    validationMethod: "DNS",
    tags: {
        ...commonTags,
        Name: `${name}-cert`,
    },
});

// Route53 validation records for ACM
const certValidationRecords: aws.route53.Record[] = [];
certificate.domainValidationOptions.apply(options => {
    const validationOptions = options;
    validationOptions.forEach((option, index) => {
        const record = new aws.route53.Record(`cert-validation-${index}`, {
            zoneId: mydomain.then(zone => zone.zoneId),
            name: option.resourceRecordName,
            type: option.resourceRecordType,
            records: [option.resourceRecordValue],
            ttl: 60,
            allowOverwrite: true,
        });
        certValidationRecords.push(record);
    });
});

// Certificate Validation
const certValidation = new aws.acm.CertificateValidation("cert-validation", {
    certificateArn: certificate.arn,
    validationRecordFqdns: certificate.domainValidationOptions.apply(options =>
        options.map(option => option.resourceRecordName)
    ),
});

// Application Load Balancer
const alb = new aws.lb.LoadBalancer("alb", {
    name: `${name}-alb`,
    loadBalancerType: "application",
    securityGroups: [loadbalancerSg.id],
    subnets: vpc.publicSubnetIds,
    enableDeletionProtection: false,
    tags: {
        ...commonTags,
        Name: `${name}-alb`,
    },
});

// Target Groups
const targetGroup1 = new aws.lb.TargetGroup("mytg1", {
    name: "mytg1",
    port: 80,
    protocol: "HTTP",
    vpcId: vpc.vpcId,
    targetType: "instance",
    deregistrationDelay: 10,
    healthCheck: {
        enabled: true,
        path: "/app1/index.html",
        port: "traffic-port",
        protocol: "HTTP",
        matcher: "200",
        interval: 30,
        timeout: 5,
        healthyThreshold: 5,
        unhealthyThreshold: 2,
    },
    stickiness: {
        type: "lb_cookie",
        enabled: true,
        cookieDuration: 3600,
    },
    tags: {
        ...commonTags,
        Name: "mytg1",
    },
});

const targetGroup2 = new aws.lb.TargetGroup("mytg2", {
    name: "mytg2",
    port: 80,
    protocol: "HTTP",
    vpcId: vpc.vpcId,
    targetType: "instance",
    deregistrationDelay: 10,
    healthCheck: {
        enabled: true,
        path: "/app2/index.html",
        port: "traffic-port",
        protocol: "HTTP",
        matcher: "200",
        interval: 30,
        timeout: 5,
        healthyThreshold: 5,
        unhealthyThreshold: 2,
    },
    stickiness: {
        type: "lb_cookie",
        enabled: true,
        cookieDuration: 3600,
    },
    tags: {
        ...commonTags,
        Name: "mytg2",
    },
});

const targetGroup3 = new aws.lb.TargetGroup("mytg3", {
    name: "mytg3",
    port: 8080,
    protocol: "HTTP",
    vpcId: vpc.vpcId,
    targetType: "instance",
    deregistrationDelay: 10,
    healthCheck: {
        enabled: true,
        path: "/login",
        port: "traffic-port",
        protocol: "HTTP",
        matcher: "200",
        interval: 30,
        timeout: 5,
        healthyThreshold: 5,
        unhealthyThreshold: 2,
    },
    stickiness: {
        type: "lb_cookie",
        enabled: true,
        cookieDuration: 3600,
    },
    tags: {
        ...commonTags,
        Name: "mytg3",
    },
});

// Target Group Attachments
app1Instances.forEach((instance, index) => {
    new aws.lb.TargetGroupAttachment(`mytg1-attachment-${index}`, {
        targetGroupArn: targetGroup1.arn,
        targetId: instance.id,
        port: 80,
    });
});

app2Instances.forEach((instance, index) => {
    new aws.lb.TargetGroupAttachment(`mytg2-attachment-${index}`, {
        targetGroupArn: targetGroup2.arn,
        targetId: instance.id,
        port: 80,
    });
});

app3Instances.forEach((instance, index) => {
    new aws.lb.TargetGroupAttachment(`mytg3-attachment-${index}`, {
        targetGroupArn: targetGroup3.arn,
        targetId: instance.id,
        port: 8080,
    });
});

// HTTP Listener (redirect to HTTPS)
const httpListener = new aws.lb.Listener("http-listener", {
    loadBalancerArn: alb.arn,
    port: 80,
    protocol: "HTTP",
    defaultActions: [{
        type: "redirect",
        redirect: {
            port: "443",
            protocol: "HTTPS",
            statusCode: "HTTP_301",
        },
    }],
});

// HTTPS Listener
const httpsListener = new aws.lb.Listener("https-listener", {
    loadBalancerArn: alb.arn,
    port: 443,
    protocol: "HTTPS",
    sslPolicy: "ELBSecurityPolicy-TLS13-1-2-Res-2021-06",
    certificateArn: certValidation.certificateArn,
    defaultActions: [{
        type: "fixed-response",
        fixedResponse: {
            contentType: "text/plain",
            messageBody: "Default Response",
            statusCode: "200",
        },
    }],
});

// Listener Rules
const listenerRuleApp1 = new aws.lb.ListenerRule("myapp1-rule", {
    listenerArn: httpsListener.arn,
    priority: 10,
    actions: [{
        type: "forward",
        targetGroupArn: targetGroup1.arn,
    }],
    conditions: [{
        pathPattern: {
            values: ["/app1*"],
        },
    }],
});

const listenerRuleApp2 = new aws.lb.ListenerRule("myapp2-rule", {
    listenerArn: httpsListener.arn,
    priority: 20,
    actions: [{
        type: "forward",
        targetGroupArn: targetGroup2.arn,
    }],
    conditions: [{
        pathPattern: {
            values: ["/app2*"],
        },
    }],
});

const listenerRuleApp3 = new aws.lb.ListenerRule("myapp3-rule", {
    listenerArn: httpsListener.arn,
    priority: 30,
    actions: [{
        type: "forward",
        targetGroupArn: targetGroup3.arn,
    }],
    conditions: [{
        pathPattern: {
            values: ["/*"],
        },
    }],
});

// Route53 DNS Record
const appsDnsRecord = new aws.route53.Record("apps-dns", {
    zoneId: mydomain.then(zone => zone.zoneId),
    name: "dns-to-db.pulumi-demos.net",
    type: "A",
    aliases: [{
        name: alb.dnsName,
        zoneId: alb.zoneId,
        evaluateTargetHealth: true,
    }],
});

// Exports
export const vpcId = vpc.vpcId;
export const vpcCidr = vpc.vpc.cidrBlock;
export const publicSubnets = vpc.publicSubnetIds;
export const privateSubnets = vpc.privateSubnetIds;
export const databaseSubnets = vpc.isolatedSubnetIds;
export const bastionPublicIp = bastionEip.publicIp;
export const albDnsName = alb.dnsName;
export const albZoneId = alb.zoneId;
export const rdsEndpoint = rdsdb.endpoint;
export const rdsAddress = rdsdb.address;
export const appUrl = pulumi.interpolate`https://dns-to-db.pulumi-demos.net`;
