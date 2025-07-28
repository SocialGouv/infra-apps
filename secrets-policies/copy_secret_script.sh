#!/bin/bash

# Script to copy buildkit-client-certs secret from buildkit-service namespace
# to all namespaces with field.cattle.io/projectId annotation

set -e

# Configuration
SOURCE_NAMESPACE="buildkit-service"
SECRET_NAME="buildkit-client-certs"
ANNOTATION_KEY="field.cattle.io/projectId"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "Starting secret copy process..."

# Check if source secret exists
if ! kubectl get secret "$SECRET_NAME" -n "$SOURCE_NAMESPACE" &>/dev/null; then
    echo -e "${RED}Error: Secret '$SECRET_NAME' not found in namespace '$SOURCE_NAMESPACE'${NC}"
    exit 1
fi

echo -e "${GREEN}Found source secret '$SECRET_NAME' in namespace '$SOURCE_NAMESPACE'${NC}"

# Get all namespaces with the specified annotation
echo "Finding namespaces with annotation '$ANNOTATION_KEY'..."
# Using kubectl with grep approach as fallback if jq is not available
if command -v jq &> /dev/null; then
    TARGET_NAMESPACES=$(kubectl get namespaces -o json | jq -r ".items[] | select(.metadata.annotations and (.metadata.annotations | has(\"$ANNOTATION_KEY\"))) | .metadata.name" | tr '\n' ' ')
else
    echo "jq not found, using alternative method..."
    TARGET_NAMESPACES=$(kubectl get namespaces -o yaml | grep -B 10 -A 1 "$ANNOTATION_KEY" | grep "^  name:" | awk '{print $2}' | tr '\n' ' ')
fi

if [ -z "$TARGET_NAMESPACES" ]; then
    echo -e "${YELLOW}No namespaces found with annotation '$ANNOTATION_KEY'${NC}"
    exit 0
fi

echo -e "${GREEN}Found target namespaces: $TARGET_NAMESPACES${NC}"

# Function to copy secret to a namespace
copy_secret_to_namespace() {
    local target_ns=$1
    
    # Skip if target namespace is the same as source
    if [ "$target_ns" = "$SOURCE_NAMESPACE" ]; then
        echo -e "${YELLOW}Skipping source namespace '$target_ns'${NC}"
        return
    fi
    
    echo "Copying secret to namespace '$target_ns'..."
    
    # Check if secret already exists in target namespace
    if kubectl get secret "$SECRET_NAME" -n "$target_ns" &>/dev/null; then
        echo -e "${YELLOW}Secret '$SECRET_NAME' already exists in namespace '$target_ns'. Updating...${NC}"
        kubectl delete secret "$SECRET_NAME" -n "$target_ns"
    fi
    
    # Copy the secret by extracting and recreating it
    kubectl get secret "$SECRET_NAME" -n "$SOURCE_NAMESPACE" -ojson \
      | jq 'del(.metadata.namespace,.metadata.resourceVersion,.metadata.uid) | .metadata.creationTimestamp=null | .metadata.selfLink=null | .metadata.annotations=null | .metadata.ownerReferences=null' |\
        kubectl apply -n "$target_ns" -f -
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}Successfully copied secret to namespace '$target_ns'${NC}"
    else
        echo -e "${RED}Failed to copy secret to namespace '$target_ns'${NC}"
    fi
}

# Copy secret to each target namespace
for namespace in $TARGET_NAMESPACES; do
    copy_secret_to_namespace "$namespace"
done

echo -e "${GREEN}Secret copy process completed!${NC}"

# Optional: Display summary
echo ""
echo "Summary:"
echo "- Source: $SECRET_NAME in $SOURCE_NAMESPACE"
echo "- Target namespaces: $TARGET_NAMESPACES"
echo "- Annotation filter: $ANNOTATION_KEY"