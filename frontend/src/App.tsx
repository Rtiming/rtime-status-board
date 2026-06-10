import { useEffect, useMemo, useState } from 'react';
import {
  Activity,
  Bug,
  Clock,
  Gauge,
  Languages,
  Layers3,
  ListChecks,
  RefreshCw,
  Server,
  TriangleAlert
} from 'lucide-react';
import { fetchDiagnostics, fetchSummary } from './api';
import { Diagnostics } from './features/diagnostics';
import { Events } from './features/events';
import { Nodes } from './features/nodes';
import { Overview } from './features/overview';
import { Projects } from './features/projects';
import { Services } from './features/services';
import { Metrics } from './features/metrics';
import { dictionary, initialLang, type Lang } from './shared/i18n';
import { StatusPill } from './shared/status';
import type {
  DiagnosticsResponse,
  SummaryResponse
} from './types';

type Tab = 'overview' | 'nodes' | 'projects' | 'services' | 'metrics' | 'events' | 'diagnostics';

function initialTab(): Tab {
  const tab = new URLSearchParams(window.location.search).get('tab');
  if (tab === 'overview' || tab === 'nodes' || tab === 'projects' || tab === 'services' || tab === 'metrics' || tab === 'events' || tab === 'diagnostics') {
    return tab;
  }
  return 'overview';
}

function initialNodeID() {
  return new URLSearchParams(window.location.search).get('node') ?? '';
}

function initialProjectID() {
  return new URLSearchParams(window.location.search).get('project') ?? '';
}

function initialServiceID() {
  return new URLSearchParams(window.location.search).get('service') ?? '';
}

export function App() {
  const [summary, setSummary] = useState<SummaryResponse | null>(null);
  const [diagnostics, setDiagnostics] = useState<DiagnosticsResponse | null>(null);
  const [error, setError] = useState<string>('');
  const [diagnosticsError, setDiagnosticsError] = useState<string>('');
  const [tab, setTab] = useState<Tab>(initialTab);
  const [loading, setLoading] = useState(false);
  const [diagnosticsLoading, setDiagnosticsLoading] = useState(false);
  const [lang, setLang] = useState<Lang>(initialLang);
  const [selectedNode, setSelectedNode] = useState(initialNodeID);
  const [selectedProject, setSelectedProject] = useState(initialProjectID);
  const [selectedService, setSelectedService] = useState(initialServiceID);
  const t = dictionary[lang];

  const load = async () => {
    setLoading(true);
    try {
      const next = await fetchSummary();
      setSummary(next);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  };

  const loadDiagnostics = async () => {
    setDiagnosticsLoading(true);
    try {
      const next = await fetchDiagnostics();
      setDiagnostics(next);
      setDiagnosticsError('');
    } catch (err) {
      setDiagnosticsError(err instanceof Error ? err.message : 'Failed to load diagnostics');
    } finally {
      setDiagnosticsLoading(false);
    }
  };

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), 15000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (tab === 'diagnostics') {
      void loadDiagnostics();
    }
  }, [tab]);

  useEffect(() => {
    window.requestAnimationFrame(() => {
      document.querySelector<HTMLElement>('.tab.active')?.scrollIntoView({ block: 'nearest', inline: 'center' });
    });
  }, [tab]);

  useEffect(() => {
    if (!summary || selectedNode) return;
    const firstMetric = summary.metrics[0]?.node_id;
    const firstNode = summary.nodes[0]?.id;
    if (firstMetric || firstNode) {
      setSelectedNode(firstMetric ?? firstNode ?? '');
    }
  }, [selectedNode, summary]);

  useEffect(() => {
    if (!summary || selectedProject) return;
    if (summary.projects[0]) {
      setSelectedProject(summary.projects[0].id);
    }
  }, [selectedProject, summary]);

  const tabs = useMemo(
    () => [
      { id: 'overview' as Tab, label: t.overview, icon: Activity },
      { id: 'nodes' as Tab, label: t.nodes, icon: Server },
      { id: 'projects' as Tab, label: t.projects, icon: Layers3 },
      { id: 'services' as Tab, label: t.services, icon: ListChecks },
      { id: 'metrics' as Tab, label: t.metrics, icon: Gauge },
      { id: 'events' as Tab, label: t.events, icon: Clock },
      { id: 'diagnostics' as Tab, label: t.diagnostics, icon: Bug }
    ],
    [t]
  );

  const switchLang = () => {
    const next = lang === 'zh' ? 'en' : 'zh';
    window.localStorage.setItem('rtime-status-lang', next);
    setLang(next);
  };

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <div className="eyebrow">{t.private}</div>
          <h1>RTime Status Board</h1>
        </div>
        <div className="top-actions">
          {summary && <StatusPill status={summary.overall} lang={lang} />}
          <button className="text-button" onClick={switchLang} type="button">
            <Languages size={16} />
            <span>{t.langButton}</span>
          </button>
          <button className="icon-button" onClick={() => void load()} title="Refresh" type="button">
            <RefreshCw className={loading ? 'spin' : ''} size={18} />
          </button>
        </div>
      </header>

      {error && (
        <div className="notice">
          <TriangleAlert size={18} />
          <span>{error}</span>
        </div>
      )}

      <nav className="tabs" aria-label="Status board views">
        {tabs.map((item) => {
          const Icon = item.icon;
          return (
            <button className={tab === item.id ? 'tab active' : 'tab'} key={item.id} onClick={() => setTab(item.id)} type="button">
              <Icon size={16} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </nav>

      {!summary ? (
        <div className="loading-panel">{t.loading}</div>
      ) : (
        <>
          {tab === 'overview' && <Overview summary={summary} lang={lang} />}
          {tab === 'nodes' && (
            <Nodes
              nodes={summary.nodes}
              lang={lang}
              selectedNodeID={selectedNode}
              onSelectNode={setSelectedNode}
              onInspect={(nodeID) => {
                setSelectedNode(nodeID);
                setTab('metrics');
              }}
            />
          )}
          {tab === 'projects' && <Projects projects={summary.projects} lang={lang} selectedProjectID={selectedProject} onSelectProject={setSelectedProject} />}
          {tab === 'services' && <Services services={summary.services} lang={lang} selectedServiceID={selectedService} onSelectService={setSelectedService} />}
          {tab === 'metrics' && <Metrics metrics={summary.metrics} lang={lang} selectedNodeID={selectedNode} onSelectNode={setSelectedNode} />}
          {tab === 'events' && <Events events={summary.events} lang={lang} />}
          {tab === 'diagnostics' && (
            <Diagnostics
              diagnostics={diagnostics}
              error={diagnosticsError}
              loading={diagnosticsLoading}
              lang={lang}
              onRefresh={() => void loadDiagnostics()}
            />
          )}
        </>
      )}
    </main>
  );
}
