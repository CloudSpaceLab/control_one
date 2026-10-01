package contentpacks

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	awsCloudTrailParserID         = "controlone.aws.cloudtrail.json.v1"
	awsCloudTrailInsightsParserID = "controlone.aws.cloudtrail.insights.json.v1"
	awsVPCFlowParserID            = "controlone.aws.vpc_flow.v1"
	awsGuardDutyParserID          = "controlone.aws.guardduty.json.v1"
	awsCloudAuditSemantics        = "aws_cloud_audit"
)

type awsCloudDetectionSpec struct {
	Detection Detection
	Rule      string
}

type awsCloudSampleSpec struct {
	CaseID      string
	SourceID    string
	ParserID    string
	Description string
	Raw         string
}

// AWSCloudAuditPack returns first-class AWS audit/security coverage for
// CloudTrail, CloudTrail Insights, VPC Flow Logs, and GuardDuty.
func AWSCloudAuditPack() (*PackContent, error) {
	detectionSpecs := awsCloudDetectionSpecs()
	files := map[string][]byte{}
	detections := make([]Detection, 0, len(detectionSpecs))
	for _, spec := range detectionSpecs {
		detections = append(detections, spec.Detection)
		files[spec.Detection.Path] = []byte(strings.TrimSpace(spec.Rule) + "\n")
	}

	samples := awsCloudSamples()
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		PackID:        "controlone.aws_cloud_audit",
		PackVersion:   "1.0.0",
		DisplayName:   "AWS Cloud Audit",
		Description:   "AWS CloudTrail, CloudTrail Insights, VPC Flow Logs, and GuardDuty parsing with high-value security detections.",
		Labels: map[string]string{
			"control_one.scope": "cloud_security",
			"cloud.provider":    "aws",
		},
		License: LicenseMetadata{SPDX: "Apache-2.0"},
		Provenance: Provenance{
			Author: "Control One",
			Sources: []string{
				"AWS CloudTrail event record schema",
				"AWS CloudTrail Insights event schema",
				"AWS VPC Flow Logs default format",
				"AWS GuardDuty finding format",
			},
		},
		Parsers:    awsCloudParserProfiles(),
		Sources:    awsCloudSourceProfiles(),
		Detections: detections,
		Samples:    make([]SampleCase, 0, len(samples)),
	}

	for _, sample := range samples {
		manifest.Samples = append(manifest.Samples, SampleCase{
			CaseID:      sample.CaseID,
			SourceID:    sample.SourceID,
			ParserID:    sample.ParserID,
			InputPath:   "samples/" + sample.CaseID + ".input.jsonl",
			GoldenPath:  "samples/" + sample.CaseID + ".golden.jsonl",
			Description: sample.Description,
		})
		input, golden, err := awsCloudSampleFiles(sample)
		if err != nil {
			return nil, err
		}
		files["samples/"+sample.CaseID+".input.jsonl"] = input
		files["samples/"+sample.CaseID+".golden.jsonl"] = golden
	}

	if err := Validate(manifest); err != nil {
		return nil, err
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal AWS cloud audit manifest: %w", err)
	}
	files["manifest.json"] = append(manifestBytes, '\n')

	return &PackContent{
		Manifest:     manifest,
		ManifestPath: "manifest.json",
		Root:         newMemoryFS(files),
	}, nil
}

func awsCloudParserProfiles() []ParserProfile {
	return []ParserProfile{
		{
			ParserID:    awsCloudTrailParserID,
			DisplayName: "AWS CloudTrail JSON Parser",
			Version:     "1.0.0",
			Stages: []ParserStage{
				{Type: StageJSON},
				{Type: StageFieldMap, Config: map[string]any{
					"mappings": map[string]any{
						"event.code":            "eventID",
						"event.action":          "eventName",
						"event.provider":        "eventSource",
						"user.name":             "userIdentity.arn",
						"user.id":               "userIdentity.principalId",
						"source.ip":             "sourceIPAddress",
						"cloud.region":          "awsRegion",
						"cloud.account.id":      "recipientAccountId",
						"error.code":            "errorCode",
						"error.message":         "errorMessage",
						"aws.mfa_authenticated": "userIdentity.sessionContext.attributes.mfaAuthenticated",
						"aws.mfa_used":          "additionalEventData.MFAUsed",
						"aws.event_category":    "eventCategory",
						"aws.management_event":  "managementEvent",
						"aws.read_only":         "readOnly",
					},
					"set": map[string]any{
						"event.kind":                   "event",
						"event.category":               "configuration",
						"event.dataset":                "aws.cloudtrail",
						"cloud.provider":               "aws",
						"control_one.vendor_semantics": awsCloudAuditSemantics,
					},
				}},
			},
		},
		{
			ParserID:    awsCloudTrailInsightsParserID,
			DisplayName: "AWS CloudTrail Insights JSON Parser",
			Version:     "1.0.0",
			Stages: []ParserStage{
				{Type: StageJSON},
				{Type: StageFieldMap, Config: map[string]any{
					"mappings": map[string]any{
						"event.code":           "eventID",
						"event.action":         "insightDetails.eventName",
						"event.provider":       "insightDetails.eventSource",
						"user.name":            "userIdentity.arn",
						"source.ip":            "sourceIPAddress",
						"cloud.region":         "awsRegion",
						"cloud.account.id":     "recipientAccountId",
						"aws.insight.type":     "insightDetails.insightType",
						"aws.insight.state":    "insightDetails.state",
						"aws.insight.baseline": "insightDetails.baseline.average",
						"aws.insight.observed": "insightDetails.insight.average",
					},
					"set": map[string]any{
						"event.kind":                   "alert",
						"event.category":               "threat",
						"event.dataset":                "aws.cloudtrail.insights",
						"cloud.provider":               "aws",
						"control_one.vendor_semantics": awsCloudAuditSemantics,
					},
				}},
			},
		},
		{
			ParserID:    awsVPCFlowParserID,
			DisplayName: "AWS VPC Flow Logs Parser",
			Version:     "1.0.0",
			Stages: []ParserStage{
				{Type: StageRegex, Config: map[string]any{
					"pattern": "^(?P<version>\\S+)\\s+(?P<account_id>\\S+)\\s+(?P<interface_id>\\S+)\\s+(?P<srcaddr>\\S+)\\s+(?P<dstaddr>\\S+)\\s+(?P<srcport>\\S+)\\s+(?P<dstport>\\S+)\\s+(?P<protocol>\\S+)\\s+(?P<packets>\\S+)\\s+(?P<bytes>\\S+)\\s+(?P<start>\\S+)\\s+(?P<end>\\S+)\\s+(?P<action>\\S+)\\s+(?P<log_status>\\S+)$",
				}},
				{Type: StageFieldMap, Config: map[string]any{
					"mappings": map[string]any{
						"cloud.account.id":     "account_id",
						"network.interface.id": "interface_id",
						"source.ip":            "srcaddr",
						"destination.ip":       "dstaddr",
						"source.port":          "srcport",
						"destination.port":     "dstport",
						"network.iana_number":  "protocol",
						"network.packets":      "packets",
						"network.bytes":        "bytes",
						"event.action":         "action",
						"event.outcome":        "log_status",
					},
					"set": map[string]any{
						"event.kind":                   "event",
						"event.category":               "network",
						"event.dataset":                "aws.vpc_flow",
						"cloud.provider":               "aws",
						"control_one.vendor_semantics": awsCloudAuditSemantics,
					},
				}},
			},
		},
		{
			ParserID:    awsGuardDutyParserID,
			DisplayName: "AWS GuardDuty Finding JSON Parser",
			Version:     "1.0.0",
			Stages: []ParserStage{
				{Type: StageJSON},
				{Type: StageFieldMap, Config: map[string]any{
					"mappings": map[string]any{
						"event.code":       "id",
						"event.action":     "type",
						"event.severity":   "severity",
						"message":          "description",
						"cloud.region":     "region",
						"cloud.account.id": "accountId",
						"resource.id":      "resource.instanceDetails.instanceId",
						"source.ip":        "service.action.networkConnectionAction.remoteIpDetails.ipAddressV4",
						"destination.port": "service.action.networkConnectionAction.localPortDetails.port",
					},
					"set": map[string]any{
						"event.kind":                   "alert",
						"event.category":               "threat",
						"event.dataset":                "aws.guardduty",
						"event.provider":               "guardduty.amazonaws.com",
						"cloud.provider":               "aws",
						"control_one.vendor_semantics": awsCloudAuditSemantics,
					},
				}},
			},
		},
	}
}

func awsCloudSourceProfiles() []SourceProfile {
	return []SourceProfile{
		awsCloudSourceProfile(
			"aws.cloudtrail", "AWS CloudTrail", "CloudTrail", "cloud_audit",
			awsCloudTrailParserID,
			[]string{
				"controlone.aws.root_account_usage",
				"controlone.aws.unauthorized_api_call",
				"controlone.aws.console_login_without_mfa",
				"controlone.aws.cloudtrail_modified",
				"controlone.aws.s3_policy_changed",
				"controlone.aws.iam_policy_changed",
				"controlone.aws.vpc_attachment_changed",
			},
			[]string{"aws.cloudtrail.root.sample"},
		),
		awsCloudSourceProfile(
			"aws.cloudtrail_insights", "AWS CloudTrail Insights", "CloudTrail Insights", "cloud_audit",
			awsCloudTrailInsightsParserID,
			[]string{"controlone.aws.cloudtrail_insight"},
			[]string{"aws.cloudtrail.insight.sample"},
		),
		awsCloudSourceProfile(
			"aws.vpc_flow", "AWS VPC Flow Logs", "VPC Flow Logs", "cloud_network",
			awsVPCFlowParserID,
			[]string{"controlone.aws.vpc_flow_rejected"},
			[]string{"aws.vpc_flow.reject.sample"},
		),
		awsCloudSourceProfile(
			"aws.guardduty", "AWS GuardDuty", "GuardDuty", "cloud_threat",
			awsGuardDutyParserID,
			[]string{"controlone.aws.guardduty_finding"},
			[]string{"aws.guardduty.finding.sample"},
		),
	}
}

func awsCloudSourceProfile(sourceID, displayName, product, sourceClass, parserID string, detectionIDs, sampleIDs []string) SourceProfile {
	return SourceProfile{
		SourceID:           sourceID,
		DisplayName:        displayName,
		Vendor:             "Amazon Web Services",
		Product:            product,
		Versions:           []string{"current"},
		SourceClass:        sourceClass,
		RiskClass:          RiskHigh,
		DataSensitivity:    SensitivityHigh,
		CollectorModes:     []string{CollectorVendorAPI, CollectorArchive, CollectorOTLP},
		ApprovalRequired:   true,
		RequiredPrivileges: []string{"read_aws_security_telemetry", "read_cloudtrail_or_security_exports"},
		ExpectedVolume: VolumeHint{
			EventsPerSecond: 250,
			BytesPerSecond:  250000,
			Burst:           "10x during incidents or control-plane automation",
		},
		RawRetentionDefault: "30d",
		Schemas: SchemaBinding{
			Primary:       SchemaOCSF,
			ExportAliases: []string{SchemaECS},
			OCSF: OCSFBinding{
				Category: "cloud_activity",
				Class:    "api_activity",
				Activity: "activity",
			},
		},
		Parsers:    []string{parserID},
		Detections: append([]string(nil), detectionIDs...),
		Samples:    append([]string(nil), sampleIDs...),
		Labels: map[string]string{
			"cloud.provider":                "aws",
			"control_one.pack_tier":         "enterprise",
			"control_one.vendor_semantics":  awsCloudAuditSemantics,
			"control_one.discovery_aliases": "aws-cloudtrail,cloudtrail,aws-cloudwatch-agent,amazon-cloudwatch-agent",
		},
		Metadata: map[string]string{
			"vendor_semantics": awsCloudAuditSemantics,
			"program":          "aws-cloudtrail",
			"parser_profile":   sourceID,
			"source_aliases":   "aws-cloudtrail,cloudtrail,aws-cloudwatch-agent,amazon-cloudwatch-agent",
		},
	}
}

func awsCloudSamples() []awsCloudSampleSpec {
	return []awsCloudSampleSpec{
		{
			CaseID:      "aws.cloudtrail.root.sample",
			SourceID:    "aws.cloudtrail",
			ParserID:    awsCloudTrailParserID,
			Description: "CloudTrail root-account console login without MFA",
			Raw:         `{"eventVersion":"1.11","eventID":"evt-root-1","eventTime":"2026-09-30T10:00:00Z","eventSource":"signin.amazonaws.com","eventName":"ConsoleLogin","awsRegion":"us-east-1","sourceIPAddress":"203.0.113.10","recipientAccountId":"123456789012","userIdentity":{"type":"Root","principalId":"123456789012","arn":"arn:aws:iam::123456789012:root","sessionContext":{"attributes":{"mfaAuthenticated":"false"}}},"additionalEventData":{"MFAUsed":"No"},"eventCategory":"Management","managementEvent":true,"readOnly":false}`,
		},
		{
			CaseID:      "aws.cloudtrail.insight.sample",
			SourceID:    "aws.cloudtrail_insights",
			ParserID:    awsCloudTrailInsightsParserID,
			Description: "CloudTrail Insights API call rate anomaly",
			Raw:         `{"eventVersion":"1.08","eventID":"evt-insight-1","eventTime":"2026-09-30T10:05:00Z","eventCategory":"Insight","awsRegion":"us-east-1","recipientAccountId":"123456789012","insightDetails":{"state":"Start","eventSource":"ec2.amazonaws.com","eventName":"RunInstances","insightType":"ApiCallRateInsight","baseline":{"average":0.2},"insight":{"average":18.0}}}`,
		},
		{
			CaseID:      "aws.vpc_flow.reject.sample",
			SourceID:    "aws.vpc_flow",
			ParserID:    awsVPCFlowParserID,
			Description: "Rejected AWS VPC flow",
			Raw:         "2 123456789012 eni-abc123 198.51.100.5 10.0.1.10 51515 22 6 10 840 1727690400 1727690460 REJECT OK",
		},
		{
			CaseID:      "aws.guardduty.finding.sample",
			SourceID:    "aws.guardduty",
			ParserID:    awsGuardDutyParserID,
			Description: "High-severity GuardDuty network finding",
			Raw:         `{"schemaVersion":"2.0","accountId":"123456789012","region":"us-east-1","id":"gd-1","type":"UnauthorizedAccess:EC2/SSHBruteForce","severity":8.2,"description":"EC2 instance is receiving SSH brute-force traffic.","resource":{"resourceType":"Instance","instanceDetails":{"instanceId":"i-0123456789abcdef0"}},"service":{"action":{"actionType":"NETWORK_CONNECTION","networkConnectionAction":{"remoteIpDetails":{"ipAddressV4":"198.51.100.5"},"localPortDetails":{"port":22}}}}}`,
		},
	}
}

func awsCloudSampleFiles(sample awsCloudSampleSpec) ([]byte, []byte, error) {
	inputLine, err := json.Marshal(ParserInput{Raw: sample.Raw})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal AWS sample input %s: %w", sample.CaseID, err)
	}
	var profile ParserProfile
	for _, candidate := range awsCloudParserProfiles() {
		if candidate.ParserID == sample.ParserID {
			profile = candidate
			break
		}
	}
	if profile.ParserID == "" {
		return nil, nil, fmt.Errorf("AWS sample %s references unknown parser %s", sample.CaseID, sample.ParserID)
	}
	compiled, err := DefaultParserRuntimeRegistry().Compile(profile)
	if err != nil {
		return nil, nil, fmt.Errorf("compile AWS parser %s: %w", profile.ParserID, err)
	}
	output, err := compiled.Parse(ParserInput{Raw: sample.Raw})
	if err != nil {
		return nil, nil, fmt.Errorf("parse AWS sample %s: %w", sample.CaseID, err)
	}
	golden := sampleGoldenRecord{
		ParserID: output.ParserID,
		Status:   output.Status,
		Fields:   output.Event.Fields,
		Labels:   output.Event.Labels,
	}
	goldenLine, err := json.Marshal(golden)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal AWS sample golden %s: %w", sample.CaseID, err)
	}
	return append(inputLine, '\n'), append(goldenLine, '\n'), nil
}

func awsCloudDetectionSpecs() []awsCloudDetectionSpec {
	return []awsCloudDetectionSpec{
		awsCloudDetection(
			"controlone.aws.root_account_usage", "AWS Root Account Usage", "high", 85,
			"detections/aws-root-account-usage.yml",
			`title: AWS Root Account Usage
status: stable
logsource:
  product: aws
  service: cloudtrail
detection:
  selection:
    user.name|contains: ':root'
  condition: selection
level: high`,
		),
		awsCloudDetection(
			"controlone.aws.unauthorized_api_call", "AWS Unauthorized API Call", "high", 80,
			"detections/aws-unauthorized-api-call.yml",
			`title: AWS Unauthorized API Call
status: stable
logsource:
  product: aws
  service: cloudtrail
detection:
  selection:
    error.code|contains:
      - AccessDenied
      - UnauthorizedOperation
  condition: selection
level: high`,
		),
		awsCloudDetection(
			"controlone.aws.console_login_without_mfa", "AWS Console Login Without MFA", "high", 82,
			"detections/aws-console-login-without-mfa.yml",
			`title: AWS Console Login Without MFA
status: stable
logsource:
  product: aws
  service: cloudtrail
detection:
  selection:
    event.action: ConsoleLogin
    aws.mfa_used: 'No'
  condition: selection
level: high`,
		),
		awsCloudDetection(
			"controlone.aws.cloudtrail_modified", "AWS CloudTrail Logging Modified", "critical", 92,
			"detections/aws-cloudtrail-modified.yml",
			`title: AWS CloudTrail Logging Modified
status: stable
logsource:
  product: aws
  service: cloudtrail
detection:
  selection:
    event.action:
      - StopLogging
      - DeleteTrail
      - UpdateTrail
      - PutEventSelectors
  condition: selection
level: critical`,
		),
		awsCloudDetection(
			"controlone.aws.s3_policy_changed", "AWS S3 Bucket Policy Changed", "high", 78,
			"detections/aws-s3-policy-changed.yml",
			`title: AWS S3 Bucket Policy Changed
status: stable
logsource:
  product: aws
  service: cloudtrail
detection:
  selection:
    event.provider: s3.amazonaws.com
    event.action:
      - PutBucketPolicy
      - DeleteBucketPolicy
      - PutBucketAcl
      - PutPublicAccessBlock
  condition: selection
level: high`,
		),
		awsCloudDetection(
			"controlone.aws.iam_policy_changed", "AWS IAM Policy Changed", "high", 84,
			"detections/aws-iam-policy-changed.yml",
			`title: AWS IAM Policy Changed
status: stable
logsource:
  product: aws
  service: cloudtrail
detection:
  selection:
    event.provider: iam.amazonaws.com
    event.action:
      - PutRolePolicy
      - PutUserPolicy
      - AttachRolePolicy
      - AttachUserPolicy
      - CreatePolicyVersion
      - SetDefaultPolicyVersion
  condition: selection
level: high`,
		),
		awsCloudDetection(
			"controlone.aws.vpc_attachment_changed", "AWS VPC Peering Or Attachment Changed", "high", 76,
			"detections/aws-vpc-attachment-changed.yml",
			`title: AWS VPC Peering Or Attachment Changed
status: stable
logsource:
  product: aws
  service: cloudtrail
detection:
  selection:
    event.provider: ec2.amazonaws.com
    event.action:
      - CreateVpcPeeringConnection
      - AcceptVpcPeeringConnection
      - AttachInternetGateway
      - CreateTransitGatewayVpcAttachment
  condition: selection
level: high`,
		),
		awsCloudDetection(
			"controlone.aws.cloudtrail_insight", "AWS CloudTrail Insight Detected", "high", 80,
			"detections/aws-cloudtrail-insight.yml",
			`title: AWS CloudTrail Insight Detected
status: stable
logsource:
  product: aws
  service: cloudtrail
detection:
  selection:
    event.dataset: aws.cloudtrail.insights
    aws.insight.state: Start
  condition: selection
level: high`,
		),
		awsCloudDetection(
			"controlone.aws.vpc_flow_rejected", "AWS VPC Flow Rejected", "medium", 55,
			"detections/aws-vpc-flow-rejected.yml",
			`title: AWS VPC Flow Rejected
status: stable
logsource:
  product: aws
  service: vpc
detection:
  selection:
    event.dataset: aws.vpc_flow
    event.action: REJECT
  condition: selection
level: medium`,
		),
		awsCloudDetection(
			"controlone.aws.guardduty_finding", "AWS GuardDuty Finding", "high", 88,
			"detections/aws-guardduty-finding.yml",
			`title: AWS GuardDuty Finding
status: stable
logsource:
  product: aws
  service: guardduty
detection:
  selection:
    event.dataset: aws.guardduty
  condition: selection
level: high`,
		),
	}
}

func awsCloudDetection(id, title, severity string, riskScore int, path, rule string) awsCloudDetectionSpec {
	return awsCloudDetectionSpec{
		Detection: Detection{
			DetectionID: id,
			Title:       title,
			Kind:        DetectionKindSigma,
			Path:        path,
			Severity:    severity,
			RiskScore:   riskScore,
			Tags:        []string{"attack.cloud", "controlone.aws"},
		},
		Rule: rule,
	}
}
