import { useState, useCallback } from 'react';
import { discoveryService, type DiscoveryResult, type IPRange } from '../services/discoveryService';

interface NodeDiscoveryProps {
  onNodesDiscovered: (nodes: DiscoveryResult['nodes']) => void;
}

export function NodeDiscovery({ onNodesDiscovered }: NodeDiscoveryProps): JSX.Element {
  const [isScanning, setIsScanning] = useState(false);
  const [scanProgress, setScanProgress] = useState(0);
  const [scanResult, setScanResult] = useState<DiscoveryResult | null>(null);
  const [customRange, setCustomRange] = useState('');
  const [selectedRanges, setSelectedRanges] = useState<IPRange[]>([]);
  const [error, setError] = useState<string | null>(null);

  const commonRanges = discoveryService.generateCommonRanges();

  const handleRangeToggle = useCallback((range: IPRange) => {
    setSelectedRanges(prev => {
      const exists = prev.some(r => r.cidr === range.cidr);
      if (exists) {
        return prev.filter(r => r.cidr !== range.cidr);
      } else {
        return [...prev, range];
      }
    });
  }, []);

  const handleAddCustomRange = useCallback(() => {
    if (!customRange.trim()) return;
    
    try {
      const ranges = discoveryService.parseIPRange(customRange.trim());
      if (ranges.length > 0) {
        setSelectedRanges(prev => [...prev, ...ranges]);
        setCustomRange('');
        setError(null);
      } else {
        setError('Invalid IP range format');
      }
    } catch (err) {
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

    } catch (err) {
      setError(err instanceof Error ? err.message : 'Discovery failed');
    } finally {
      setIsScanning(false);
      setScanProgress(0);
    }
  }, [selectedRanges, onNodesDiscovered]);

  const handleQuickScan = useCallback(async () => {
    setSelectedRanges(commonRanges.slice(0, 2)); // Select first 2 common ranges
    await new Promise(resolve => setTimeout(resolve, 100));
    await handleScan();
  }, [commonRanges, handleScan]);

  return (
    <div className="compact-panel">
      <h3>🔍 Smart Node Discovery</h3>
      
      <div className="discovery-controls">
        <div className="form-field">
          <label>Quick Scan Common Networks</label>
          <button 
            type="button" 
            className="primary-button" 
            onClick={handleQuickScan}
            disabled={isScanning}
          >
            {isScanning ? 'Scanning...' : 'Quick Scan'}
          </button>
          <small>Scans 192.168.1.0/24 and 192.168.0.0/24</small>
        </div>

        <div className="form-field">
          <label>Select IP Ranges</label>
          <div className="range-list">
            {commonRanges.map((range) => (
              <label key={range.cidr} className="checkbox-option">
                <input
                  type="checkbox"
                  checked={selectedRanges.some(r => r.cidr === range.cidr)}
                  onChange={() => handleRangeToggle(range)}
                  disabled={isScanning}
                />
                <span>{range.cidr}</span>
                <small>{range.start} - {range.end}</small>
              </label>
            ))}
          </div>
        </div>

        <div className="form-field">
          <label>Custom IP Range</label>
          <div className="compact-form">
            <input
              type="text"
              placeholder="e.g., 192.168.1.0/24 or 10.0.0.1-10.0.0.50"
              value={customRange}
              onChange={(e) => setCustomRange(e.target.value)}
              disabled={isScanning}
            />
            <button
              type="button"
              className="ghost-button"
              onClick={handleAddCustomRange}
              disabled={isScanning || !customRange.trim()}
            >
              Add Range
            </button>
          </div>
        </div>

        {selectedRanges.length > 0 && (
          <div className="selected-ranges">
            <label>Selected Ranges ({selectedRanges.length})</label>
            <div className="range-tags">
              {selectedRanges.map((range) => (
                <span key={range.cidr} className="range-tag">
                  {range.cidr}
                  <button
                    type="button"
                    onClick={() => handleRangeToggle(range)}
                    disabled={isScanning}
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          </div>
        )}

        {error && <div className="form-error">{error}</div>}

        <button
          type="button"
          className="primary-button"
          onClick={handleScan}
          disabled={isScanning || selectedRanges.length === 0}
        >
          {isScanning ? 'Scanning...' : 'Start Discovery'}
        </button>

        {isScanning && (
          <div className="scan-progress">
            <div className="progress-bar">
              <div 
                className="progress-fill" 
                style={{ width: `${scanProgress}%` }}
              />
            </div>
            <small>Scanning... {scanProgress}%</small>
          </div>
        )}

        {scanResult && (
          <div className="scan-results">
            <h4>Discovery Results</h4>
            <div className="result-stats">
              <div className="stat">
                <strong>{scanResult.onlineCount}</strong>
                <span>Nodes Online</span>
              </div>
              <div className="stat">
                <strong>{scanResult.totalScanned}</strong>
                <span>IPs Scanned</span>
              </div>
              <div className="stat">
                <strong>{scanResult.scanDuration}ms</strong>
                <span>Duration</span>
              </div>
            </div>
            
            {scanResult.nodes.length > 0 && (
              <div className="discovered-nodes">
                <h5>Discovered Nodes ({scanResult.nodes.length})</h5>
                <div className="node-list">
                  {scanResult.nodes.map((node, index) => (
                    <div key={index} className="discovered-node">
                      <div className={`status-dot status-${node.status}`} />
                      <div className="node-info">
                        <strong>{node.ip}</strong>
                        <small>Port {node.port}</small>
                        {node.hostname && <small>{node.hostname}</small>}
                      </div>
                      <button
                        type="button"
                        className="ghost-button"
                        onClick={() => onNodesDiscovered([node])}
                      >
                        Add Node
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
