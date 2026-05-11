package compliance

import "sort"

// ControlMapping describes a single control within a compliance framework.
// Applicability is HIPAA-specific in practice ("required" vs "addressable")
// and left empty for other frameworks.
type ControlMapping struct {
	Framework     string
	ControlID     string
	Title         string
	Description   string
	Applicability string `json:",omitempty"`
}

// FrameworkControls is the core control set for each supported framework.
// Not exhaustive — each framework's full spec is larger; what's catalogued
// here is the subset platform automation can meaningfully attest against.
var FrameworkControls = map[string][]ControlMapping{
	"SOC2": {
		{Framework: "SOC2", ControlID: "CC1.1", Title: "COSO Principle 1 — Integrity and ethical values", Description: "The entity demonstrates a commitment to integrity and ethical values."},
		{Framework: "SOC2", ControlID: "CC1.2", Title: "COSO Principle 2 — Board oversight", Description: "The board exercises oversight of internal control."},
		{Framework: "SOC2", ControlID: "CC1.3", Title: "COSO Principle 3 — Authority and responsibility", Description: "Management establishes structures, reporting lines, and appropriate authorities."},
		{Framework: "SOC2", ControlID: "CC1.4", Title: "COSO Principle 4 — Competence", Description: "The entity demonstrates a commitment to attract, develop, and retain competent individuals."},
		{Framework: "SOC2", ControlID: "CC1.5", Title: "COSO Principle 5 — Accountability", Description: "The entity holds individuals accountable for internal control responsibilities."},
		{Framework: "SOC2", ControlID: "CC2.1", Title: "Information quality", Description: "The entity obtains and uses relevant, quality information to support internal control."},
		{Framework: "SOC2", ControlID: "CC2.2", Title: "Internal communication", Description: "The entity internally communicates information necessary to support internal control."},
		{Framework: "SOC2", ControlID: "CC2.3", Title: "External communication", Description: "The entity communicates with external parties regarding matters affecting internal control."},
		{Framework: "SOC2", ControlID: "CC3.1", Title: "Risk identification — objectives", Description: "The entity specifies objectives with clarity to enable identification of risks."},
		{Framework: "SOC2", ControlID: "CC3.2", Title: "Risk identification — analysis", Description: "The entity identifies risks to the achievement of its objectives."},
		{Framework: "SOC2", ControlID: "CC3.3", Title: "Fraud risk", Description: "The entity considers the potential for fraud in assessing risks."},
		{Framework: "SOC2", ControlID: "CC3.4", Title: "Change risk", Description: "The entity identifies and assesses changes that could significantly impact internal control."},
		{Framework: "SOC2", ControlID: "CC4.1", Title: "Monitoring activities", Description: "The entity selects, develops, and performs ongoing and separate evaluations."},
		{Framework: "SOC2", ControlID: "CC4.2", Title: "Deficiency communication", Description: "The entity evaluates and communicates internal control deficiencies."},
		{Framework: "SOC2", ControlID: "CC5.1", Title: "Control activities — selection", Description: "The entity selects and develops control activities that contribute to risk mitigation."},
		{Framework: "SOC2", ControlID: "CC5.2", Title: "Control activities — technology", Description: "The entity selects and develops technology general controls."},
		{Framework: "SOC2", ControlID: "CC5.3", Title: "Control activities — policies", Description: "The entity deploys control activities through policies and procedures."},
		{Framework: "SOC2", ControlID: "CC6.1", Title: "Logical access — implementation", Description: "The entity implements logical access security software, infrastructure, and architectures over protected information assets."},
		{Framework: "SOC2", ControlID: "CC6.2", Title: "Logical access — registration and authorization", Description: "Prior to issuing system credentials, the entity registers and authorizes new internal and external users."},
		{Framework: "SOC2", ControlID: "CC6.3", Title: "Logical access — modifications and removal", Description: "The entity authorizes, modifies, or removes access based on roles, responsibilities, or the system design."},
		{Framework: "SOC2", ControlID: "CC6.6", Title: "Logical access — boundary protection", Description: "The entity implements logical access security measures to protect against threats from outside its system boundaries."},
		{Framework: "SOC2", ControlID: "CC6.7", Title: "Restricted physical access", Description: "The entity restricts the transmission, movement, and removal of information."},
		{Framework: "SOC2", ControlID: "CC6.8", Title: "Malicious software", Description: "The entity implements controls to prevent or detect and act upon the introduction of unauthorized or malicious software."},
		{Framework: "SOC2", ControlID: "CC7.1", Title: "Vulnerability identification", Description: "The entity uses detection and monitoring procedures to identify changes to configurations that result in vulnerabilities."},
		{Framework: "SOC2", ControlID: "CC7.2", Title: "Anomaly monitoring", Description: "The entity monitors system components and the operation of those components for anomalies."},
		{Framework: "SOC2", ControlID: "CC7.3", Title: "Security event evaluation", Description: "The entity evaluates security events to determine whether they could or have resulted in a failure."},
		{Framework: "SOC2", ControlID: "CC7.4", Title: "Incident response", Description: "The entity responds to identified security incidents by executing a defined incident response program."},
		{Framework: "SOC2", ControlID: "CC7.5", Title: "Recovery", Description: "The entity identifies, develops, and implements activities to recover from identified security incidents."},
		{Framework: "SOC2", ControlID: "CC8.1", Title: "Change management", Description: "The entity authorizes, designs, develops, configures, documents, tests, approves, and implements changes."},
		{Framework: "SOC2", ControlID: "CC9.1", Title: "Risk mitigation — disruptions", Description: "The entity identifies, selects, and develops risk mitigation activities for risks arising from potential business disruptions."},
		{Framework: "SOC2", ControlID: "CC9.2", Title: "Risk mitigation — vendors", Description: "The entity assesses and manages risks associated with vendors and business partners."},
		{Framework: "SOC2", ControlID: "A1.1", Title: "Availability — capacity", Description: "The entity maintains, monitors, and evaluates current processing capacity and use of system components."},
		{Framework: "SOC2", ControlID: "A1.2", Title: "Availability — environmental protections", Description: "The entity authorizes, designs, develops, implements, operates, approves, maintains, and monitors environmental protections."},
		{Framework: "SOC2", ControlID: "A1.3", Title: "Availability — recovery", Description: "The entity tests recovery plan procedures supporting system recovery."},
	},
	"ISO27001": {
		{Framework: "ISO27001", ControlID: "A.5.1", Title: "Policies for information security", Description: "Information security policy and topic-specific policies are defined, approved, published, and reviewed."},
		{Framework: "ISO27001", ControlID: "A.5.10", Title: "Acceptable use of information", Description: "Rules for acceptable use and procedures for handling information are identified and implemented."},
		{Framework: "ISO27001", ControlID: "A.5.15", Title: "Access control", Description: "Rules to control physical and logical access to information are established."},
		{Framework: "ISO27001", ControlID: "A.5.30", Title: "ICT readiness for business continuity", Description: "ICT readiness is planned, implemented, and tested based on business continuity objectives."},
		{Framework: "ISO27001", ControlID: "A.6.1", Title: "Screening", Description: "Background verification checks on candidates are carried out prior to joining."},
		{Framework: "ISO27001", ControlID: "A.6.1-AML", Title: "Customer screening — AML/sanctions", Description: "Customers are screened against sanctions watchlists during onboarding and on periodic review."},
		{Framework: "ISO27001", ControlID: "A.6.3", Title: "Information security awareness", Description: "Personnel receive information-security awareness, education, and training."},
		{Framework: "ISO27001", ControlID: "A.7.1", Title: "Physical security perimeter", Description: "Security perimeters are defined and used to protect areas containing information."},
		{Framework: "ISO27001", ControlID: "A.7.2", Title: "Physical entry", Description: "Secure areas are protected by appropriate entry controls and access points."},
		{Framework: "ISO27001", ControlID: "A.8.3", Title: "Information access restriction", Description: "Access to information and other associated assets is restricted in accordance with the access control policy."},
		{Framework: "ISO27001", ControlID: "A.8.5", Title: "Secure authentication", Description: "Secure authentication technologies and procedures are implemented."},
		{Framework: "ISO27001", ControlID: "A.8.7", Title: "Protection against malware", Description: "Protection against malware is implemented and supported by user awareness."},
		{Framework: "ISO27001", ControlID: "A.8.8", Title: "Management of technical vulnerabilities", Description: "Information about technical vulnerabilities is obtained, exposure evaluated, and measures taken."},
		{Framework: "ISO27001", ControlID: "A.8.15", Title: "Logging", Description: "Logs that record activities, exceptions, faults, and other relevant events are produced, stored, protected, and analysed."},
		{Framework: "ISO27001", ControlID: "A.8.16", Title: "Monitoring activities", Description: "Networks, systems, and applications are monitored for anomalous behaviour."},
		{Framework: "ISO27001", ControlID: "A.8.18", Title: "Use of privileged utility programs", Description: "The use of utility programs that might be capable of overriding controls is restricted and tightly controlled."},
		{Framework: "ISO27001", ControlID: "A.8.20", Title: "Networks security", Description: "Networks and network devices are secured, managed, and controlled to protect information."},
		{Framework: "ISO27001", ControlID: "A.8.23", Title: "Web filtering", Description: "Access to external websites is managed to reduce exposure to malicious content."},
		{Framework: "ISO27001", ControlID: "A.8.24", Title: "Use of cryptography", Description: "Rules for the effective use of cryptography, including key management, are defined and implemented."},
		{Framework: "ISO27001", ControlID: "A.8.28", Title: "Secure coding", Description: "Secure coding principles are applied to software development."},
		{Framework: "ISO27001", ControlID: "A.16.1", Title: "Management of information security incidents", Description: "Information-security events are reported and assessed; weaknesses are reported promptly."},
	},
	"HIPAA": {
		// Administrative Safeguards — 45 CFR § 164.308
		{Framework: "HIPAA", ControlID: "164.308(a)(1)", Title: "Security Management Process", Description: "Implement policies and procedures to prevent, detect, contain, and correct security violations.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.308(a)(2)", Title: "Assigned Security Responsibility", Description: "Identify the security official responsible for the development and implementation of policies.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.308(a)(3)", Title: "Workforce Security", Description: "Implement policies to ensure workforce members have appropriate access to ePHI.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.308(a)(4)", Title: "Information Access Management", Description: "Implement policies for authorizing access to ePHI consistent with the Privacy Rule.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.308(a)(5)", Title: "Security Awareness and Training", Description: "Implement a security awareness and training program for all members of its workforce.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.308(a)(5)(ii)(D)", Title: "Password Management", Description: "Procedures for creating, changing, and safeguarding passwords.", Applicability: "addressable"},
		{Framework: "HIPAA", ControlID: "164.308(a)(6)", Title: "Security Incident Procedures", Description: "Implement policies and procedures to address security incidents.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.308(a)(7)", Title: "Contingency Plan", Description: "Establish policies for responding to emergencies that damage systems containing ePHI.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.308(a)(8)", Title: "Evaluation", Description: "Perform a periodic technical and nontechnical evaluation in response to environmental or operational changes.", Applicability: "required"},

		// Physical Safeguards — 45 CFR § 164.310
		{Framework: "HIPAA", ControlID: "164.310(a)", Title: "Facility Access Controls", Description: "Limit physical access to electronic information systems and facilities they reside in.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.310(b)", Title: "Workstation Use", Description: "Specify the proper functions to be performed and the manner in which those functions are to be performed.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.310(c)", Title: "Workstation Security", Description: "Implement physical safeguards for all workstations that access ePHI.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.310(d)", Title: "Device and Media Controls", Description: "Govern the receipt and removal of hardware and electronic media containing ePHI.", Applicability: "required"},

		// Technical Safeguards — 45 CFR § 164.312
		{Framework: "HIPAA", ControlID: "164.312(a)(1)", Title: "Access Control", Description: "Implement technical policies and procedures to allow access only to authorized persons or software programs.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.312(b)", Title: "Audit Controls", Description: "Implement hardware, software, and procedural mechanisms that record and examine activity.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.312(c)", Title: "Integrity", Description: "Protect ePHI from improper alteration or destruction.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.312(d)", Title: "Person or Entity Authentication", Description: "Verify that a person or entity seeking access to ePHI is the one claimed.", Applicability: "required"},
		{Framework: "HIPAA", ControlID: "164.312(e)(1)", Title: "Transmission Security", Description: "Implement technical security measures to guard against unauthorized access to ePHI being transmitted over a network.", Applicability: "required"},
	},
	"PCI-DSS": {
		{Framework: "PCI-DSS", ControlID: "1.1", Title: "Network security controls", Description: "Processes and mechanisms for installing and maintaining network security controls are defined and understood."},
		{Framework: "PCI-DSS", ControlID: "1.2", Title: "Network segmentation", Description: "NSCs are configured and maintained to restrict network traffic to only what is necessary."},
		{Framework: "PCI-DSS", ControlID: "2.1", Title: "Secure configurations", Description: "Processes and mechanisms for applying secure configurations to all system components are defined."},
		{Framework: "PCI-DSS", ControlID: "2.2", Title: "Configuration standards", Description: "System components are configured and managed securely."},
		{Framework: "PCI-DSS", ControlID: "4.2", Title: "Strong cryptography in transit", Description: "PAN is protected with strong cryptography during transmission over open, public networks."},
		{Framework: "PCI-DSS", ControlID: "4.2.1", Title: "Cleartext transmission disallowed", Description: "PAN is rendered unreadable or never transmitted in cleartext over public networks."},
		{Framework: "PCI-DSS", ControlID: "6.3", Title: "Vulnerabilities identified and addressed", Description: "Security vulnerabilities are identified and addressed."},
		{Framework: "PCI-DSS", ControlID: "6.3.3", Title: "System components are protected from known vulnerabilities", Description: "All system components are protected from known vulnerabilities by installing applicable security patches."},
		{Framework: "PCI-DSS", ControlID: "6.5", Title: "Changes to all system components", Description: "Changes to all system components are managed securely."},
		{Framework: "PCI-DSS", ControlID: "6.7", Title: "Bespoke and custom software", Description: "Bespoke and custom software is developed securely."},
		{Framework: "PCI-DSS", ControlID: "7.1", Title: "Restrict access by need-to-know", Description: "Access to system components and data is appropriately restricted."},
		{Framework: "PCI-DSS", ControlID: "8.2", Title: "User identification", Description: "User identification and related accounts for users and administrators are strictly managed."},
		{Framework: "PCI-DSS", ControlID: "8.3", Title: "Strong authentication", Description: "Strong authentication for users and administrators is established and managed."},
		{Framework: "PCI-DSS", ControlID: "8.3.4", Title: "Account lockout", Description: "Repeated authentication attempts are limited."},
		{Framework: "PCI-DSS", ControlID: "8.3.6", Title: "Password complexity", Description: "Authentication factors meet complexity requirements."},
		{Framework: "PCI-DSS", ControlID: "8.3.9", Title: "Password lifetime", Description: "Authentication factors are changed at established intervals."},
		{Framework: "PCI-DSS", ControlID: "10.2", Title: "Audit logs implemented", Description: "Audit logs are implemented to support the detection of anomalies and suspicious activity."},
		{Framework: "PCI-DSS", ControlID: "10.5", Title: "Audit log history retained", Description: "Audit log history is retained and available for analysis."},
		{Framework: "PCI-DSS", ControlID: "10.6", Title: "Time synchronization", Description: "Time-synchronization mechanisms support consistent time settings across all systems."},
		{Framework: "PCI-DSS", ControlID: "11.3", Title: "External and internal vulnerabilities are identified", Description: "External and internal vulnerabilities are regularly identified, prioritized, and addressed."},
		{Framework: "PCI-DSS", ControlID: "12.3", Title: "Risks to the cardholder data environment are formally identified", Description: "Risks are formally identified, evaluated, and managed."},
	},
	"GDPR": {
		{Framework: "GDPR", ControlID: "Art.5", Title: "Principles relating to processing of personal data", Description: "Personal data shall be processed lawfully, fairly, and in a transparent manner; collected for specified purposes; minimised; accurate; storage-limited; with integrity and confidentiality; and accountable."},
		{Framework: "GDPR", ControlID: "Art.13", Title: "Information to be provided where personal data are collected from the data subject", Description: "Provide identity of controller, purposes, recipients, retention, and rights at the time of collection."},
		{Framework: "GDPR", ControlID: "Art.14", Title: "Information where personal data have not been obtained from the data subject", Description: "Provide source, categories, purposes, retention, and rights when data is collected indirectly."},
		{Framework: "GDPR", ControlID: "Art.25", Title: "Data protection by design and by default", Description: "Implement appropriate technical and organisational measures, both at the time of determination of means and at the time of processing."},
		{Framework: "GDPR", ControlID: "Art.28", Title: "Processor", Description: "Processing by a processor is governed by a contract or other legal act binding the processor to the controller."},
		{Framework: "GDPR", ControlID: "Art.32", Title: "Security of processing", Description: "Implement appropriate technical and organisational measures to ensure a level of security appropriate to the risk, including pseudonymisation, encryption, ongoing confidentiality and integrity, resilience, and recovery."},
		{Framework: "GDPR", ControlID: "Art.33", Title: "Notification of a personal data breach to the supervisory authority", Description: "Notify the supervisory authority of a personal data breach within 72 hours where feasible."},
		{Framework: "GDPR", ControlID: "Art.34", Title: "Communication of a personal data breach to the data subject", Description: "Communicate the breach to the data subject without undue delay when likely to result in a high risk."},
	},
	"BSA": {
		{Framework: "BSA", ControlID: "AML-SCREEN", Title: "Customer sanctions screening", Description: "Customer identity is matched against OFAC and equivalent sanctions watchlists prior to account activation."},
	},
}

// ListFrameworks returns a sorted list of supported framework names.
func ListFrameworks() []string {
	keys := make([]string, 0, len(FrameworkControls))
	for k := range FrameworkControls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ControlByID looks up a single control within a framework. Returns the zero
// value and false if not found.
func ControlByID(framework, controlID string) (ControlMapping, bool) {
	for _, c := range FrameworkControls[framework] {
		if c.ControlID == controlID {
			return c, true
		}
	}
	return ControlMapping{}, false
}
