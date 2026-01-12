import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState, useCallback } from 'react';
import { discoveryService } from '../services/discoveryService';
export function NodeDiscovery({ onNodesDiscovered }) {
    const [isScanning, setIsScanning] = useState(false);
    const [scanProgress, setScanProgress] = useState(0);
    const [scanResult, setScanResult] = useState(null);
    const [customRange, setCustomRange] = useState('');
    const [selectedRanges, setSelectedRanges] = useState([]);
    const [error, setError] = useState(null);
    const commonRanges = discoveryService.generateCommonRanges();
    const handleRangeToggle = useCallback((range) => {
        setSelectedRanges(prev => {
            const exists = prev.some(r => r.cidr === range.cidr);
            if (exists) {
                return prev.filter(r => r.cidr !== range.cidr);
            }
            else {
                return [...prev, range];
            }
        });
    }, []);
    const handleAddCustomRange = useCallback(() => {
        if (!customRange.trim())
            return;
        try {
            const ranges = discoveryService.parseIPRange(customRange.trim());
            if (ranges.length > 0) {
                setSelectedRanges(prev => [...prev, ...ranges]);
                setCustomRange('');
                setError(null);
            }
            else {
                setError('Invalid IP range format');
            }
        }
        catch (err) {
            setError('Failed to parse IP range');
        }
    }, [customRange]);
    const handleScan = useCallback(async () => {
        if (selectedRanges.length === 0) {
            setError('Please select at least one IP range to scan');
            return;
        }
        setIsScanning(true);
        setScanProgress(0);
        setError(null);
        try {
            // Simulate progress updates
            const progressInterval = setInterval(() => {
                setScanProgress(prev => Math.min(prev + 10, 90));
            }, 500);
            const result = await discoveryService.discoverNodes(selectedRanges, {
                ports: [22, 80, 443, 8080, 8443],
                timeout: 2000,
                maxConcurrent: 15
            });
            clearInterval(progressInterval);
            setScanProgress(100);
            setScanResult(result);
            onNodesDiscovered(result.nodes);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'Discovery failed');
        }
        finally {
            setIsScanning(false);
            setScanProgress(0);
        }
    }, [selectedRanges, onNodesDiscovered]);
    const handleQuickScan = useCallback(async () => {
        setSelectedRanges(commonRanges.slice(0, 2)); // Select first 2 common ranges
        await new Promise(resolve => setTimeout(resolve, 100));
        await handleScan();
    }, [commonRanges, handleScan]);
    return (_jsxs("div", { className: "compact-panel", children: [_jsx("h3", { children: "\uD83D\uDD0D Smart Node Discovery" }), _jsxs("div", { className: "discovery-controls", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Quick Scan Common Networks" }), _jsx("button", { type: "button", className: "primary-button", onClick: handleQuickScan, disabled: isScanning, children: isScanning ? 'Scanning...' : 'Quick Scan' }), _jsx("small", { children: "Scans 192.168.1.0/24 and 192.168.0.0/24" })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Select IP Ranges" }), _jsx("div", { className: "range-list", children: commonRanges.map((range) => (_jsxs("label", { className: "checkbox-option", children: [_jsx("input", { type: "checkbox", checked: selectedRanges.some(r => r.cidr === range.cidr), onChange: () => handleRangeToggle(range), disabled: isScanning }), _jsx("span", { children: range.cidr }), _jsxs("small", { children: [range.start, " - ", range.end] })] }, range.cidr))) })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Custom IP Range" }), _jsxs("div", { className: "compact-form", children: [_jsx("input", { type: "text", placeholder: "e.g., 192.168.1.0/24 or 10.0.0.1-10.0.0.50", value: customRange, onChange: (e) => setCustomRange(e.target.value), disabled: isScanning }), _jsx("button", { type: "button", className: "ghost-button", onClick: handleAddCustomRange, disabled: isScanning || !customRange.trim(), children: "Add Range" })] })] }), selectedRanges.length > 0 && (_jsxs("div", { className: "selected-ranges", children: [_jsxs("label", { children: ["Selected Ranges (", selectedRanges.length, ")"] }), _jsx("div", { className: "range-tags", children: selectedRanges.map((range) => (_jsxs("span", { className: "range-tag", children: [range.cidr, _jsx("button", { type: "button", onClick: () => handleRangeToggle(range), disabled: isScanning, children: "\u00D7" })] }, range.cidr))) })] })), error && _jsx("div", { className: "form-error", children: error }), _jsx("button", { type: "button", className: "primary-button", onClick: handleScan, disabled: isScanning || selectedRanges.length === 0, children: isScanning ? 'Scanning...' : 'Start Discovery' }), isScanning && (_jsxs("div", { className: "scan-progress", children: [_jsx("div", { className: "progress-bar", children: _jsx("div", { className: "progress-fill", style: { width: `${scanProgress}%` } }) }), _jsxs("small", { children: ["Scanning... ", scanProgress, "%"] })] })), scanResult && (_jsxs("div", { className: "scan-results", children: [_jsx("h4", { children: "Discovery Results" }), _jsxs("div", { className: "result-stats", children: [_jsxs("div", { className: "stat", children: [_jsx("strong", { children: scanResult.onlineCount }), _jsx("span", { children: "Nodes Online" })] }), _jsxs("div", { className: "stat", children: [_jsx("strong", { children: scanResult.totalScanned }), _jsx("span", { children: "IPs Scanned" })] }), _jsxs("div", { className: "stat", children: [_jsxs("strong", { children: [scanResult.scanDuration, "ms"] }), _jsx("span", { children: "Duration" })] })] }), scanResult.nodes.length > 0 && (_jsxs("div", { className: "discovered-nodes", children: [_jsxs("h5", { children: ["Discovered Nodes (", scanResult.nodes.length, ")"] }), _jsx("div", { className: "node-list", children: scanResult.nodes.map((node, index) => (_jsxs("div", { className: "discovered-node", children: [_jsx("div", { className: `status-dot status-${node.status}` }), _jsxs("div", { className: "node-info", children: [_jsx("strong", { children: node.ip }), _jsxs("small", { children: ["Port ", node.port] }), node.hostname && _jsx("small", { children: node.hostname })] }), _jsx("button", { type: "button", className: "ghost-button", onClick: () => onNodesDiscovered([node]), children: "Add Node" })] }, index))) })] }))] }))] })] }));
}
