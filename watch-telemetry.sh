#!/bin/bash

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Get container IDs
SERVER_ID=$(container list | grep 192.168.64.6 | awk '{print $1}')
CLIENT_ID=$(container list | grep 192.168.64.7 | awk '{print $1}')

# Terminal width
TERM_WIDTH=$(tput cols)
HALF_WIDTH=$((TERM_WIDTH / 2 - 2))

clear

# Function to print centered title
print_title() {
    local text="$1"
    local color="$2"
    local padding=$(( (TERM_WIDTH - ${#text}) / 2 ))
    printf "%${padding}s" ""
    echo -e "${color}${BOLD}${text}${NC}"
}

# Function to print separator
print_separator() {
    local char="${1:-=}"
    printf "${CYAN}%${TERM_WIDTH}s${NC}\n" | tr ' ' "$char"
}

# Function to truncate text
truncate_text() {
    local text="$1"
    local width="$2"
    if [ ${#text} -gt $width ]; then
        echo "${text:0:$((width-3))}..."
    else
        printf "%-${width}s" "$text"
    fi
}

# Function to show logs side by side
show_side_by_side() {
    local lines="$1"

    # Get server logs
    local server_logs=$(container exec $SERVER_ID tail -${lines} /tmp/server-inst.log 2>/dev/null)
    # Get client logs
    local client_logs=$(container exec $CLIENT_ID tail -${lines} /tmp/client-inst.log 2>/dev/null)

    # Convert to arrays
    IFS=$'\n' read -rd '' -a server_array <<<"$server_logs"
    IFS=$'\n' read -rd '' -a client_array <<<"$client_logs"

    # Get max lines
    local max_lines=${#server_array[@]}
    if [ ${#client_array[@]} -gt $max_lines ]; then
        max_lines=${#client_array[@]}
    fi

    # Print side by side
    for ((i=0; i<max_lines; i++)); do
        local server_line="${server_array[$i]}"
        local client_line="${client_array[$i]}"

        # Color code based on log level
        if [[ "$server_line" == *"[BUS]"* ]]; then
            server_line="${CYAN}${server_line}${NC}"
        elif [[ "$server_line" == *"[ADAPTER]"* ]]; then
            server_line="${GREEN}${server_line}${NC}"
        elif [[ "$server_line" == *"[EMITTER]"* ]]; then
            server_line="${YELLOW}${server_line}${NC}"
        elif [[ "$server_line" == *"[REQUEST]"* ]]; then
            server_line="${MAGENTA}${server_line}${NC}"
        elif [[ "$server_line" == *"[RESPONSE]"* ]]; then
            server_line="${BLUE}${server_line}${NC}"
        fi

        if [[ "$client_line" == *"[TRANSMIT]"* ]]; then
            client_line="${YELLOW}${client_line}${NC}"
        elif [[ "$client_line" == *"[RECEIVE]"* ]]; then
            client_line="${GREEN}${client_line}${NC}"
        elif [[ "$client_line" == *"[NETWORK]"* ]]; then
            client_line="${CYAN}${client_line}${NC}"
        elif [[ "$client_line" == *"[STATS]"* ]]; then
            client_line="${MAGENTA}${client_line}${NC}"
        fi

        # Truncate and print
        local server_truncated=$(truncate_text "$server_line" $HALF_WIDTH)
        local client_truncated=$(truncate_text "$client_line" $HALF_WIDTH)

        echo -e "$server_truncated │ $client_truncated"
    done
}

# Function to get stats
get_stats() {
    # Server stats
    local server_requests=$(container exec $SERVER_ID grep -c "REQUEST #" /tmp/server-inst.log 2>/dev/null || echo "0")
    local server_events=$(container exec $SERVER_ID grep -c "Received event from bus" /tmp/server-inst.log 2>/dev/null || echo "0")

    # Client stats
    local client_requests=$(container exec $CLIENT_ID grep -c "REQUEST #" /tmp/client-inst.log 2>/dev/null || echo "0")
    local client_success=$(container exec $CLIENT_ID grep -c "✅ REQUEST" /tmp/client-inst.log 2>/dev/null || echo "0")

    echo -e "${GREEN}Server:${NC} $server_requests requests | $server_events events processed ${BLUE}│${NC} ${GREEN}Client:${NC} $client_requests requests | $client_success successful"
}

# Main monitoring loop
echo ""
print_title "🔬 PIPELINE TELEMETRY MONITOR" "$GREEN"
echo ""
print_separator

echo ""
echo -e "${CYAN}Server:${NC} $SERVER_ID (192.168.64.6:8080)"
echo -e "${CYAN}Client:${NC} $CLIENT_ID (192.168.64.7)"
echo ""
print_separator "─"
echo ""

# Watch mode
echo -e "${YELLOW}Starting real-time monitoring... (Ctrl+C to stop)${NC}"
echo ""
sleep 2

while true; do
    clear

    # Header
    echo ""
    print_title "🔬 PIPELINE TELEMETRY MONITOR - $(date '+%H:%M:%S')" "$GREEN"
    echo ""
    print_separator
    echo ""

    # Stats
    get_stats
    echo ""
    print_separator "─"
    echo ""

    # Column headers
    printf "${BOLD}${GREEN}%-${HALF_WIDTH}s${NC} ${BOLD}│${NC} ${BOLD}${BLUE}%-${HALF_WIDTH}s${NC}\n" "SERVER (192.168.64.6:8080)" "CLIENT (192.168.64.7)"
    print_separator "─"
    echo ""

    # Show last 20 lines side by side
    show_side_by_side 20

    echo ""
    print_separator
    echo ""
    echo -e "${CYAN}Legend:${NC}"
    echo -e "  ${CYAN}[BUS]${NC}      - Event bus operations"
    echo -e "  ${GREEN}[ADAPTER]${NC}  - HTTP server adapter"
    echo -e "  ${YELLOW}[EMITTER]${NC}  - HTTP client emitter"
    echo -e "  ${MAGENTA}[REQUEST]${NC}  - HTTP request details"
    echo -e "  ${BLUE}[RESPONSE]${NC} - HTTP response generation"
    echo -e "  ${YELLOW}[TRANSMIT]${NC} - Client transmission"
    echo -e "  ${GREEN}[RECEIVE]${NC}  - Client reception"
    echo -e "  ${CYAN}[NETWORK]${NC}  - Network operations"
    echo -e "  ${MAGENTA}[STATS]${NC}    - Performance statistics"
    echo ""
    echo -e "${YELLOW}Refreshing every 2 seconds... Press Ctrl+C to stop${NC}"

    sleep 2
done
