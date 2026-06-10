import { Layers3, Server, ShieldCheck, Signal } from 'lucide-react';
import { Metric, Panel, Row } from '../../shared/components';
import { dictionary, type Lang } from '../../shared/i18n';
import { statusLabel } from '../../shared/status';
import type { SummaryResponse } from '../../types';

export function Overview({ summary, lang }: { summary: SummaryResponse; lang: Lang }) {
  const t = dictionary[lang];
  return (
    <section className="stack">
      <div className="metrics-grid">
        <Metric icon={ShieldCheck} label={t.overall} value={statusLabel[lang][summary.overall]} status={summary.overall} />
        <Metric icon={Server} label={t.nodes} value={String(summary.nodes.length)} />
        <Metric icon={Layers3} label={t.projects} value={String(summary.projects.length)} />
        <Metric icon={Signal} label={t.checks} value={String(summary.services.length)} />
      </div>
      <div className="split">
        <Panel title={t.nodeState}>
          <div className="mini-list">
            {summary.nodes.map((node) => (
              <Row key={node.id} title={node.name} subtitle={node.role} status={node.status} meta={node.detail} />
            ))}
          </div>
        </Panel>
        <Panel title={t.projectState}>
          <div className="mini-list">
            {summary.projects.map((project) => (
              <Row key={project.id} title={project.name} subtitle={project.summary} status={project.status} meta={project.detail} />
            ))}
          </div>
        </Panel>
      </div>
    </section>
  );
}
