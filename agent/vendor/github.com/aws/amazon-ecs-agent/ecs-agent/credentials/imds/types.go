// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package imds

const (
	// CredentialCodeSuccess is the info file Code for a credential the provider
	// successfully assumed the role for and wrote to IMDS.
	CredentialCodeSuccess = "Success"

	// CredentialCodeAccessDenied is the info file Code for a credential the
	// provider could not assume the role for, because the role's trust policy
	// rejects it or the AWS account is suspended.
	CredentialCodeAccessDenied = "AccessDenied"
)

// NamespaceInfo represents the parsed info file from an iam-ecs-* namespace.
// JSON tags match the IMDS response format.
type NamespaceInfo struct {
	LastUpdated     string                        `json:"LastUpdated"`
	TaskCredentials map[string]TaskCredentialInfo `json:"TaskCredentials"`
}

// TaskCredentialInfo represents a single entry in the namespace info file.
type TaskCredentialInfo struct {
	Code    string `json:"Code"`
	RoleArn string `json:"RoleARN"`
}

// TaskCredential represents a task credential retrieved from IMDS.
type TaskCredential struct {
	TaskID          string
	RoleType        string
	RoleArn         string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      string
}

// AccessDeniedIAMRole identifies a task's IAM role that the provider was denied to assume,
// as reported by a namespace info file entry with Code AccessDenied. It carries
// no credential material because the provider wrote none.
type AccessDeniedIAMRole struct {
	TaskID   string
	RoleType string
	RoleArn  string
}

// ScanResult holds the outcome of a single IMDS credentials scan.
type ScanResult struct {
	// Credentials are the task credentials retrieved from IMDS.
	Credentials []TaskCredential
	// AccessDeniedRoles are the roles the provider was denied to assume.
	// A consumer uses them to attribute a stale credential to that denial,
	// as opposed to a broken delivery path.
	AccessDeniedRoles []AccessDeniedIAMRole
}

// imdsCredential is used internally by the scanner to deserialize IMDS
// credential files, which use different field names than TaskCredential
// (e.g. "Token" vs SessionToken). JSON tags match the IMDS response format.
type imdsCredential struct {
	AccessKeyId     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	Token           string `json:"Token"`
	Expiration      string `json:"Expiration"`
}
