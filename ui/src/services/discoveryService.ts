export interface DiscoveredNode {
  ip: string;
  hostname?: string;
  os?: string;
  port: number;
  status: 'online' | 'offline';
  responseTime?: number;
}

export interface DiscoveryResult {
  nodes: DiscoveredNode[];
  scannedRanges: string[];
  totalScanned: number;
  onlineCount: number;
  scanDuration: number;
}

export interface IPRange {
  start: string;
  end: string;
  cidr: string;
}

class DiscoveryService {
  private static instance: DiscoveryService;

  static getInstance(): DiscoveryService {
    if (!DiscoveryService.instance) {
      DiscoveryService.instance = new DiscoveryService();
    }
    return DiscoveryService.instance;
  }

  /**
   * Parse various IP range formats
   */
  parseIPRange(input: string): IPRange[] {
    const ranges: IPRange[] = [];
    
    // Handle CIDR notation (e.g., 192.168.1.0/24)
    if (input.includes('/')) {
      const [network, prefixLength] = input.split('/');
      const prefix = parseInt(prefixLength, 10);
      if (prefix >= 24 && prefix <= 30) {
        ranges.push({
          start: this.getFirstIP(network, prefix),
          end: this.getLastIP(network, prefix),
          cidr: input
        });
      }
    }
    
    // Handle range notation (e.g., 192.168.1.1-192.168.1.50)
    else if (input.includes('-')) {
      const [start, end] = input.split('-').map(s => s.trim());
      ranges.push({ start, end, cidr: `${start}-${end}` });
    }
    
    // Handle single IP
    else if (this.isValidIP(input)) {
      ranges.push({ start: input, end: input, cidr: input });
    }
    
    return ranges;
  }

  /**
   * Generate common network ranges for discovery
   */
  generateCommonRanges(): IPRange[] {
    const commonRanges: IPRange[] = [
      // Private networks
      { start: '192.168.1.1', end: '192.168.1.254', cidr: '192.168.1.0/24' },
      { start: '192.168.0.1', end: '192.168.0.254', cidr: '192.168.0.0/24' },
      { start: '10.0.0.1', end: '10.0.0.254', cidr: '10.0.0.0/24' },
      { start: '172.16.0.1', end: '172.16.0.254', cidr: '172.16.0.0/24' },
    ];
    
    return commonRanges;
  }

  /**
   * Discover nodes in IP ranges with port scanning
   */
  async discoverNodes(ranges: IPRange[], options: {
    ports?: number[];
    timeout?: number;
    maxConcurrent?: number;
  } = {}): Promise<DiscoveryResult> {
    const startTime = Date.now();
    const {
      ports = [22, 80, 443, 8080, 8443], // Common ports
      timeout = 2000,
      maxConcurrent = 20
    } = options;

    const allNodes: DiscoveredNode[] = [];
    const scannedRanges: string[] = [];
    let totalScanned = 0;
    let onlineCount = 0;

    // Generate all IPs to scan
    const ipsToScan: string[] = [];
    for (const range of ranges) {
      const ips = this.generateIPs(range.start, range.end);
      ipsToScan.push(...ips);
      scannedRanges.push(range.cidr);
    }

    // Batch scan IPs
    for (let i = 0; i < ipsToScan.length; i += maxConcurrent) {
      const batch = ipsToScan.slice(i, i + maxConcurrent);
      const batchPromises = batch.map(ip => 
        this.scanIP(ip, ports, timeout)
      );

      try {
        const batchResults = await Promise.allSettled(batchPromises);
        batchResults.forEach((result) => {
          if (result.status === 'fulfilled' && result.value) {
            allNodes.push(result.value);
            if (result.value.status === 'online') {
              onlineCount++;
            }
          }
        });
      } catch (error) {
        console.warn('Batch scan failed:', error);
      }

      totalScanned += batch.length;
      
      // Small delay to prevent overwhelming the network
      await new Promise(resolve => setTimeout(resolve, 100));
    }

    const scanDuration = Date.now() - startTime;

    return {
      nodes: allNodes,
      scannedRanges,
      totalScanned,
      onlineCount,
      scanDuration
    };
  }

  /**
   * Scan a single IP for open ports
   */
  private async scanIP(ip: string, ports: number[], timeout: number): Promise<DiscoveredNode | null> {
    try {
      // Try to fetch a simple HTTP request first
      const response = await this.quickHTTPCheck(ip, timeout);
      if (response) {
        return response;
      }

      // Fallback to port scanning simulation
      for (const port of ports) {
        if (await this.isPortOpen(ip, port, timeout)) {
          return {
            ip,
            port,
            status: 'online',
            responseTime: timeout
          };
        }
      }

      return {
        ip,
        port: ports[0],
        status: 'offline'
      };
    } catch (error) {
      return {
        ip,
        port: ports[0],
        status: 'offline'
      };
    }
  }

  /**
   * Quick HTTP check for common web services
   */
  private async quickHTTPCheck(ip: string, timeout: number): Promise<DiscoveredNode | null> {
    const urls = [
      `http://${ip}:8443`,
      `https://${ip}:8443`,
      `http://${ip}:80`,
      `http://${ip}:8080`
    ];

    for (const url of urls) {
      try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), timeout);

        const response = await fetch(url, {
          method: 'HEAD',
          signal: controller.signal,
          mode: 'no-cors'
        });

        clearTimeout(timeoutId);

        // If we get here, the port is likely open
        const port = url.includes(':8443') ? 8443 : url.includes(':8080') ? 8080 : 80;
        
        return {
          ip,
          port,
          status: 'online',
          hostname: ip,
          responseTime: timeout
        };
      } catch (error) {
        // Expected to fail due to CORS, but indicates port might be open
        if (error instanceof Error && error.name === 'AbortError') {
          continue;
        }
      }
    }

    return null;
  }

  /**
   * Simulate port check (in real implementation, this would use WebSockets or server-side scanning)
   */
  private async isPortOpen(ip: string, port: number, timeout: number): Promise<boolean> {
    // Simulate port check with random results for demo
    // In production, this would use actual network scanning
    return new Promise((resolve) => {
      setTimeout(() => {
        // Simulate ~30% chance of port being open for demo
        resolve(Math.random() < 0.3);
      }, Math.random() * timeout);
    });
  }

  /**
   * Generate all IPs in a range
   */
  private generateIPs(start: string, end: string): string[] {
    const ips: string[] = [];
    const startParts = start.split('.').map(Number);
    const endParts = end.split('.').map(Number);

    for (let i = startParts[3]; i <= endParts[3]; i++) {
      ips.push(`${startParts[0]}.${startParts[1]}.${startParts[2]}.${i}`);
    }

    return ips;
  }

  /**
   * Validate IP address format
   */
  private isValidIP(ip: string): boolean {
    const parts = ip.split('.');
    return parts.length === 4 && parts.every(part => {
      const num = parseInt(part, 10);
      return num >= 0 && num <= 255;
    });
  }

  /**
   * Get first IP in CIDR range
   */
  private getFirstIP(network: string, prefixLength: number): string {
    const parts = network.split('.').map(Number);
    if (prefixLength === 24) {
      return `${parts[0]}.${parts[1]}.${parts[2]}.1`;
    }
    return network; // Simplified for demo
  }

  /**
   * Get last IP in CIDR range
   */
  private getLastIP(network: string, prefixLength: number): string {
    const parts = network.split('.').map(Number);
    if (prefixLength === 24) {
      return `${parts[0]}.${parts[1]}.${parts[2]}.254`;
    }
    return network; // Simplified for demo
  }
}

export const discoveryService = DiscoveryService.getInstance();
