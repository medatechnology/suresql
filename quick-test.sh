#!/bin/bash

# SureSQL Quick Test Script
# This script performs basic validation of all improvements

set -e

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
BASE_URL="${SURESQL_URL:-http://localhost:8080}"
AUTH_USER="${SURESQL_USER:-admin}"
AUTH_PASS="${SURESQL_PASS:-password}"

echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║   SureSQL Quick Test Suite            ║${NC}"
echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo ""

# Counters
PASSED=0
FAILED=0

# Test function
run_test() {
    local test_name="$1"
    local test_cmd="$2"
    local expected="$3"

    echo -n "Testing ${test_name}... "

    if result=$(eval "$test_cmd" 2>&1); then
        if [ -z "$expected" ] || echo "$result" | grep -q "$expected"; then
            echo -e "${GREEN}✓ PASSED${NC}"
            ((PASSED++))
            return 0
        else
            echo -e "${RED}✗ FAILED${NC}"
            echo -e "${YELLOW}  Expected: $expected${NC}"
            echo -e "${YELLOW}  Got: $result${NC}"
            ((FAILED++))
            return 1
        fi
    else
        echo -e "${RED}✗ FAILED${NC}"
        echo -e "${YELLOW}  Error: $result${NC}"
        ((FAILED++))
        return 1
    fi
}

echo -e "${BLUE}Phase 1: Basic Connectivity${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Test 1: Server is running
run_test "Server Connectivity" \
    "curl -sf -o /dev/null -w '%{http_code}' $BASE_URL/health" \
    "200"

# Test 2: Health endpoint
run_test "Health Endpoint" \
    "curl -sf $BASE_URL/health" \
    '"status":"ok"'

# Test 3: Readiness endpoint
run_test "Readiness Endpoint" \
    "curl -sf $BASE_URL/ready" \
    '"status"'

echo ""
echo -e "${BLUE}Phase 2: Monitoring Endpoints${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Test 4: Metrics endpoint (with auth)
run_test "Metrics Endpoint" \
    "curl -sf -u $AUTH_USER:$AUTH_PASS $BASE_URL/monitoring/metrics" \
    '"connections_active"'

# Test 5: Pool metrics
run_test "Pool Metrics" \
    "curl -sf -u $AUTH_USER:$AUTH_PASS $BASE_URL/monitoring/metrics/pool" \
    '"active_connections"'

# Test 6: Token metrics
run_test "Token Metrics" \
    "curl -sf -u $AUTH_USER:$AUTH_PASS $BASE_URL/monitoring/metrics/tokens" \
    '"tokens_active"'

# Test 7: Alerts endpoint
run_test "Alerts Endpoint" \
    "curl -sf -u $AUTH_USER:$AUTH_PASS $BASE_URL/monitoring/alerts" \
    '"alerts"'

# Test 8: Alert stats
run_test "Alert Stats" \
    "curl -sf -u $AUTH_USER:$AUTH_PASS $BASE_URL/monitoring/alerts/stats" \
    '"total_alerts"'

# Test 9: Detailed health
run_test "Detailed Health" \
    "curl -sf -u $AUTH_USER:$AUTH_PASS $BASE_URL/monitoring/health/detailed" \
    '"status"'

echo ""
echo -e "${BLUE}Phase 3: Load Testing${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Test 10: Concurrent requests (if ab is available)
if command -v ab &> /dev/null; then
    echo -n "Testing Concurrent Requests (100 req, 10 concurrent)... "
    if ab -n 100 -c 10 -q "$BASE_URL/health" 2>&1 | grep -q "Failed requests:.*0"; then
        echo -e "${GREEN}✓ PASSED${NC}"
        ((PASSED++))
    else
        echo -e "${YELLOW}⚠ WARNING - Some requests failed${NC}"
    fi
else
    echo -e "${YELLOW}⊘ SKIPPED - Apache Bench not installed${NC}"
fi

echo ""
echo -e "${BLUE}Phase 4: Feature Validation${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Test 11: Check metrics are being collected
echo -n "Testing Metrics Collection... "
METRICS=$(curl -sf -u "$AUTH_USER:$AUTH_PASS" "$BASE_URL/monitoring/metrics" 2>/dev/null)
if echo "$METRICS" | jq -e '.data.uptime' &>/dev/null; then
    UPTIME=$(echo "$METRICS" | jq -r '.data.uptime')
    echo -e "${GREEN}✓ PASSED${NC} (Uptime: $UPTIME)"
    ((PASSED++))
else
    echo -e "${RED}✗ FAILED${NC}"
    ((FAILED++))
fi

# Test 12: Check pool is tracking connections
echo -n "Testing Pool Tracking... "
POOL=$(curl -sf -u "$AUTH_USER:$AUTH_PASS" "$BASE_URL/monitoring/metrics/pool" 2>/dev/null)
if echo "$POOL" | jq -e '.data.max_pool_size' &>/dev/null; then
    MAX_POOL=$(echo "$POOL" | jq -r '.data.max_pool_size')
    ACTIVE=$(echo "$POOL" | jq -r '.data.active_connections')
    echo -e "${GREEN}✓ PASSED${NC} (Pool: $ACTIVE/$MAX_POOL)"
    ((PASSED++))
else
    echo -e "${RED}✗ FAILED${NC}"
    ((FAILED++))
fi

# Test 13: Check alert system is initialized
echo -n "Testing Alert System... "
ALERTS=$(curl -sf -u "$AUTH_USER:$AUTH_PASS" "$BASE_URL/monitoring/alerts/stats" 2>/dev/null)
if echo "$ALERTS" | jq -e '.data.thresholds' &>/dev/null; then
    WARNING=$(echo "$ALERTS" | jq -r '.data.thresholds.pool_warning')
    CRITICAL=$(echo "$ALERTS" | jq -r '.data.thresholds.pool_critical')
    echo -e "${GREEN}✓ PASSED${NC} (Thresholds: ${WARNING}% / ${CRITICAL}%)"
    ((PASSED++))
else
    echo -e "${RED}✗ FAILED${NC}"
    ((FAILED++))
fi

echo ""
echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║   Test Results                         ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${GREEN}✓ Passed: $PASSED${NC}"
echo -e "  ${RED}✗ Failed: $FAILED${NC}"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}╔════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║   ✓ ALL TESTS PASSED!                  ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${GREEN}🎉 SureSQL is working correctly!${NC}"
    echo ""
    echo "Next steps:"
    echo "  1. Review metrics: curl -u $AUTH_USER:**** $BASE_URL/monitoring/metrics | jq"
    echo "  2. Check alerts: curl -u $AUTH_USER:**** $BASE_URL/monitoring/alerts | jq"
    echo "  3. Run full test suite: ./test_integration.sh"
    echo "  4. Read documentation: MONITORING_FEATURES.md"
    exit 0
else
    echo -e "${RED}╔════════════════════════════════════════╗${NC}"
    echo -e "${RED}║   ✗ SOME TESTS FAILED                  ║${NC}"
    echo -e "${RED}╚════════════════════════════════════════╝${NC}"
    echo ""
    echo "Troubleshooting:"
    echo "  1. Check if server is running"
    echo "  2. Verify database connection: curl $BASE_URL/ready"
    echo "  3. Check credentials: AUTH_USER=$AUTH_USER AUTH_PASS=****"
    echo "  4. Review server logs for errors"
    echo "  5. See TESTING_GUIDE.md for detailed troubleshooting"
    exit 1
fi
