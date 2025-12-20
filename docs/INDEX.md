# Documentation Index

This document provides an organized index of all documentation in the `docs/` folder, grouped by category and ordered chronologically within each category.

## Table of Contents

1. [Core Implementation Documentation](#core-implementation-documentation)
2. [Deployment Guides](#deployment-guides)
3. [EntryPoint & Validation](#entrypoint--validation)
4. [Bundler & UserOperation Processing](#bundler--useroperation-processing)
5. [Debugging & Diagnostics](#debugging--diagnostics)
6. [Bug Fixes & Issues](#bug-fixes--issues)
7. [Configuration & Setup](#configuration--setup)
8. [Testing & Integration](#testing--integration)
9. [Key Management](#key-management)

---

## Core Implementation Documentation

**Most Recent First:**

- **USER_OPERATION_HANDLING.md** (Dec 5, 2025)
  - Comprehensive overview of the UserOperation handling implementation
  - Architecture, components, data flow, and design decisions

- **USEROP_TO_TRANSACTION_FLOW.md**
  - Detailed flow diagram of UserOperation lifecycle
  - From submission to block inclusion

- **USEROP_ACCOUNT_CREATION_SEQUENCE.md** (Dec 3, 2025)
  - Account creation flow for UserOperations with initCode
  - EntryPoint behavior and account deployment

- **USEROP_VALIDATION_PROCESS.md**
  - Step-by-step validation process
  - Signature recovery, simulation, and error handling

- **USEROP_HANDLING_ANALYSIS.md**
  - Analysis of UserOperation handling mechanisms
  - Pool management and bundling strategies

---

## Deployment Guides

- **REDEPLOY_INSTRUCTIONS.md** (Dec 5, 2025)
  - Step-by-step redeployment guide
  - Docker build, ECR push, and service restart

- **AWS_DEPLOYMENT.md**
  - Complete AWS deployment guide
  - EC2 setup, Docker configuration, service files

- **FLOW_TESTNET_DEPLOYMENT.md**
  - Flow Testnet specific deployment
  - Network configuration and contract addresses

- **DEPLOYMENT_AND_TESTING.md** (Dec 5, 2025)
  - Deployment procedures and testing workflows
  - Verification steps and troubleshooting

- **REMOTE_DEPLOYMENT.md**
  - Remote deployment procedures
  - SSH, file transfer, and remote execution

- **REBUILD_AND_REDEPLOY.md**
  - Rebuild and redeploy procedures
  - Code changes and deployment workflow

- **UPDATE_SERVICE_FILE_ONLY.md** (Dec 3, 2025)
  - Updating service file without full redeploy
  - Quick configuration changes

---

## EntryPoint & Validation

- **ENTRYPOINT_DEPLOYMENT.md**
  - EntryPoint contract deployment guide
  - Contract addresses and versions

- **ENTRYPOINT_SIMULATIONS_UPDATE.md**
  - EntryPointSimulations contract implementation
  - Support for EntryPoint v0.7+ validation

- **ENTRYPOINT_SIMULATIONS_VERIFICATION.md**
  - Verifying EntryPointSimulations contract
  - Function existence and configuration

- **ENTRYPOINT_SIMULATIONS_MISSING_FUNCTION.md**
  - Root cause analysis for missing functions
  - Contract verification issues

- **ADD_ENTRYPOINT_SIMULATIONS_FLAG.md**
  - Adding EntryPointSimulations configuration flag
  - Implementation details

- **ENTRYPOINT_SENDERCREATOR_INVESTIGATION.md**
  - Investigating senderCreator() function
  - EntryPoint version verification

- **ENTRYPOINT_DIAGNOSTICS.md**
  - Diagnostic procedures for EntryPoint
  - Validation debugging

- **ENTRYPOINT_DEBUGGING_ENHANCEMENTS.md**
  - Enhanced debugging for EntryPoint validation
  - Logging improvements

- **ENTRYPOINT_TRACE_COMMAND.md**
  - Using trace commands for EntryPoint debugging
  - debug_traceCall usage

- **CRITICAL_FEEDBACK_ANALYSIS.md**
  - Analysis of EntryPoint v0.9 understanding
  - Version compatibility issues

- **SIMULATEVALIDATION_FUNCTION_CHECK.md**
  - Checking simulateValidation function
  - Function existence verification

---

## Bundler & UserOperation Processing

- **BUNDLER_USEROP_REMOVAL_FIX.md** (Nov 30, 2025)
  - Fix for UserOps removed before transaction submission
  - Pool management correction

- **BUNDLER_NOT_INCLUDING_USEROPS.md**
  - Issue: Bundler not including UserOperations in blocks
  - Root cause and resolution

- **BUNDLER_INTERVAL_DECISION.md**
  - Decision on bundler interval configuration
  - Timing and performance considerations

- **BUNDLER_DIAGNOSTIC_COMMANDS.md**
  - Diagnostic commands for bundler debugging
  - Log filtering and monitoring

---

## Debugging & Diagnostics

- **CHECK_LOGS_GUIDE.md**
  - How to check gateway logs for UserOperation validation
  - Log filtering and analysis

- **CHECK_VALIDATION_LOGS.md** (Nov 30, 2025)
  - Checking UserOp validation logs
  - Specific log patterns and commands

- **DIAGNOSTIC_LOG_COMMAND.md**
  - Diagnostic log command for EntryPoint validation
  - Specific log queries

- **DIAGNOSTIC_WITHOUT_REDEPLOY.md**
  - Diagnostic commands that don't require redeploy
  - Runtime debugging

- **DEBUG_TRACECALL_GUIDE.md**
  - Using debug_traceCall to debug EntryPoint validation
  - Trace analysis

- **REAL_TIME_USEROP_MONITORING.md** (Nov 30, 2025)
  - Real-time UserOperation monitoring
  - Live log watching

- **VERIFY_USEROP_INCLUSION.md** (Nov 30, 2025)
  - Verifying UserOperation inclusion
  - Checking if UserOps are processed

- **DIAGNOSE_MISSING_USEROP.md** (Nov 30, 2025)
  - Diagnosing missing UserOp processing
  - Troubleshooting steps

- **FINAL_DIAGNOSTICS.md**
  - Final diagnostics for EntryPoint validation failure
  - Comprehensive debugging

- **CURRENT_DEBUG_STATUS.md**
  - Current debug status for EntryPoint validation failure
  - Status tracking

- **ROOT_CAUSE_ANALYSIS.md**
  - Root cause analysis documents
  - Issue investigation

- **ROOT_CAUSE_SUMMARY.md**
  - Summary of root cause findings
  - Key insights

- **LOG_FILTERING_COMMANDS.md**
  - Log filtering commands
  - Useful grep patterns

- **TROUBLESHOOTING_EMPTY_LOGS.md**
  - Troubleshooting empty logs
  - Log visibility issues

- **GATEWAY_PARSING_DEBUG.md**
  - Gateway initCode parsing debug
  - Parsing issues

- **INITCODE_PARSING_DEBUG_PLAN.md**
  - InitCode parsing debug plan
  - Debugging strategy

- **INITCODE_ANALYSIS.md**
  - InitCode analysis from logs
  - Data extraction

- **LOCAL_TESTING_SUMMARY.md**
  - Local testing summary with RPC response logging
  - Testing procedures

---

## Bug Fixes & Issues

- **STALE_NONCE_BUG_FIX.md** (Dec 2, 2025)
  - Fix for stale nonce bug
  - Nonce calculation correction

- **DATABASE_PERSISTENCE_CRITICAL_FIX.md** (Dec 3, 2025)
  - Critical fix for database persistence issue
  - Data loss prevention

- **HANDLEOPS_ENCODING_FIX.md**
  - Fix for handleOps ABI encoding
  - Encoding correction

- **HASH_CALCULATION_FIX.md**
  - Fix for UserOp hash calculation
  - Hash formula correction

- **ZERO_HASH_ISSUE.md**
  - Zero hash issue documentation
  - Hash generation problems

- **REBUILD_ZERO_HASH_FIX.md**
  - Rebuild for zero hash fix
  - Fix implementation

- **AA13_ERROR_DIAGNOSIS.md** (Dec 2, 2025)
  - AA13 error diagnosis and solution
  - Account creation error handling

- **ADDRESSED_EMPTY_REVERT_ISSUE.md**
  - Addressed empty revert issue
  - Revert data handling

- **DUPLICATE_USEROP_EXPLANATION.md**
  - Duplicate UserOp error explanation
  - Duplicate detection

- **SENDERCREATOR_ISSUE.md**
  - senderCreator issue documentation
  - Function call problems

- **FACTORY_ADDRESS_MISMATCH.md**
  - Factory address mismatch analysis
  - Address verification

- **RPC_SYNC_ISSUE.md**
  - RPC sync issue documentation
  - Synchronization problems

- **BLOCK_INDEXING_LAG_ISSUE.md** (Dec 3, 2025)
  - Block indexing lag issue
  - Indexing performance

---

## Configuration & Setup

- **ALIGNMENT_VERIFICATION.md**
  - Alignment verification for ERC-4337 UserOperation process
  - Configuration validation

- **VERIFY_ENTRYPOINT_SIMULATIONS_CONFIG.md**
  - Verify EntryPointSimulations configuration
  - Config validation

- **ENHANCED_LOGGING_UPDATE.md**
  - Enhanced logging for EntryPoint validation debugging
  - Logging improvements

- **REVERT_DECODING_IMPLEMENTATION.md**
  - Revert decoding implementation
  - Error message parsing

- **REVERT_DECODING_IMPROVEMENTS.md**
  - Revert decoding improvements
  - Enhanced error handling

- **REVERT_DECODING_LOG_FILTER.md**
  - Revert decoding log filter
  - Log filtering for reverts

- **UPDATED_UNDERSTANDING.md**
  - Updated understanding documents
  - Knowledge updates

---

## Testing & Integration

- **PRIVY_TEST_PLAN.md**
  - Privy + wagmi test plan for Flow EVM Gateway
  - Frontend integration testing

- **OPENZEPPELIN_PAYMASTER.md**
  - OpenZeppelin Paymaster implementation guide
  - Paymaster setup and configuration

- **PAYMASTER_VALIDATION.md**
  - Paymaster signature validation
  - Validation procedures

- **FRONTEND_RESPONSE.md**
  - Gateway response: UserOperation validation status
  - Frontend communication

- **FRONTEND_UPDATE_RESPONSE.md**
  - Gateway response: EntryPoint validation debugging
  - Frontend updates

- **QUICK_SUMMARY.md**
  - Quick summary of EntryPoint validation issue
  - Issue overview

---

## Key Management

- **KEY_MANAGEMENT.md**
  - Key management best practices
  - Security guidelines

- **KEYS_NOT_LOADING.md** (Nov 30, 2025)
  - Signing keys not loading after adding
  - Key loading issues

- **SIGNING_KEYS_ISSUE.md** (Nov 30, 2025)
  - Signing keys issue documentation
  - Key management problems

- **DIAGNOSE_KEY_LOADING.md** (Nov 30, 2025)
  - Diagnose key loading issue
  - Troubleshooting steps

- **VERIFY_KEY_MATCH.md** (Nov 30, 2025)
  - Verify key match
  - Key validation

- **VERIFY_KEY_MATCH_STEPS.md** (Nov 30, 2025)
  - Verify key match steps
  - Step-by-step validation

- **VERIFY_COA_KEY_MATCH.md** (Nov 30, 2025)
  - Verify COA key match
  - COA key validation

- **SIGNATURE_VALIDATION_ISSUE.md**
  - Signature validation issue
  - Validation problems

- **SIGNATURE_VALIDATION_ANSWERS.md**
  - Signature validation answers
  - Q&A documentation

- **SIGNATURE_FORMAT_ANALYSIS.md**
  - Signature format analysis
  - Format verification

---

## Version-Specific Documentation

- **VERSION_testnet-v1-entrypoint-simulations-fix.md**
  - Version-specific documentation for testnet v1 EntryPointSimulations fix
  - Version release notes

---

## How to Use This Index

1. **By Category**: Browse the categories above to find documentation related to your task
2. **By Date**: Within each category, documents are listed with most recent first
3. **By Topic**: Use the table of contents to jump to specific areas
4. **Search**: Use your editor's search function to find specific keywords across all docs

## Document Status

- **Active**: Documents that describe current implementation and procedures
- **Historical**: Documents that describe past issues, fixes, or deprecated approaches
- **Reference**: Documents that serve as ongoing reference material

Most documents in this folder are **active** and describe current implementation, debugging procedures, or deployment guides. Some documents may be **historical** and describe issues that have been resolved.

---

*Last Updated: December 5, 2025*

