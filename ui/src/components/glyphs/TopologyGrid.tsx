import StatusDot, { State } from './StatusDot';
import './glyphs.css';

export interface TopologyNode {
  id: string;
  hostname?: string;
  state: State;
  hint?: string; // tooltip line
}

interface Props {
  nodes: TopologyNode[];
  onNodeClick?: (node: TopologyNode) => void;
}

// TopologyGrid renders the whole fleet as colour dots. Hover for tooltip,
// click to drill in. Auto-fills based on viewport so this scales from 5
// nodes to thousands without code changes.
export default function TopologyGrid({ nodes, onNodeClick }: Props) {
  return (
    <div className="glyph-topology-grid" role="grid">
      {nodes.map((node) => (
        <button
          key={node.id}
          type="button"
          className="glyph-topology-node"
          title={node.hint ?? node.hostname ?? node.id}
          onClick={() => onNodeClick?.(node)}
          style={{ background: 'transparent', border: 'none' }}
        >
          <StatusDot state={node.state} title={node.hostname ?? node.id} />
        </button>
      ))}
    </div>
  );
}
