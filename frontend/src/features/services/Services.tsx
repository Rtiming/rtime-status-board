import { useEffect, useState } from 'react';
import { Activity, Clock, Layers3, Server, TriangleAlert } from 'lucide-react';
import { fetchServiceChecks, fetchServiceDetail } from '../../api';
import { Metric, MetricSummaryCard, Row, SubPanel } from '../../shared/components';
import { formatTime } from '../../shared/format';
import { dictionary, type Lang } from '../../shared/i18n';
import { StatusDot, StatusPill, statusLabel } from '../../shared/status';
import type { ServiceCheckHistoryResponse, ServiceDetailResponse, ServiceView } from '../../types';

export function Services({
  services,
  lang,
  selectedServiceID,
  onSelectService
}: {
  services: ServiceView[];
  lang: Lang;
  selectedServiceID: string;
  onSelectService: (serviceID: string) => void;
}) {
  const t = dictionary[lang];
  const [detail, setDetail] = useState<ServiceDetailResponse | null>(null);
  const [detailError, setDetailError] = useState('');
  const [detailLoading, setDetailLoading] = useState(false);
  const selectedService = services.find((service) => service.id === selectedServiceID) ?? services[0];
  const selectedID = selectedService?.id ?? selectedServiceID;

  useEffect(() => {
    if (services.length === 0) return;
    if (!selectedServiceID || !services.some((service) => service.id === selectedServiceID)) {
      onSelectService(services[0].id);
    }
  }, [onSelectService, selectedServiceID, services]);

  useEffect(() => {
    if (!selectedID) {
      setDetail(null);
      return;
    }
    let cancelled = false;
    const loadService = async () => {
      setDetailLoading(true);
      try {
        const next = await fetchServiceDetail(selectedID);
        if (!cancelled) {
          setDetail(next);
          setDetailError('');
        }
      } catch (err) {
        if (!cancelled) {
          setDetailError(err instanceof Error ? err.message : t.serviceError);
        }
      } finally {
        if (!cancelled) {
          setDetailLoading(false);
        }
      }
    };
    void loadService();
    return () => {
      cancelled = true;
    };
  }, [selectedID, t.serviceError]);

  return (
    <section className="stack">
      <section className="table-wrap service-table">
        <table>
          <thead>
            <tr>
              <th>{t.service}</th>
              <th>{t.status}</th>
              <th>{t.node}</th>
              <th>{t.project}</th>
              <th>{t.kind}</th>
              <th>{t.latency}</th>
              <th>{t.detail}</th>
            </tr>
          </thead>
          <tbody>
            {services.map((service) => (
              <tr className={service.id === selectedID ? 'selected-row' : ''} key={service.id}>
                <td>
                  <button className="table-link" onClick={() => onSelectService(service.id)} type="button">
                    <strong>{service.name}</strong>
                    <span>{service.critical ? t.critical : t.standard}</span>
                  </button>
                </td>
                <td><StatusPill status={service.status} lang={lang} /></td>
                <td>{service.node_id}</td>
                <td>{service.project_id}</td>
                <td>{service.kind}</td>
                <td>{service.response_time_ms > 0 ? `${service.response_time_ms}ms` : '-'}</td>
                <td>{service.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      {detailError && (
        <div className="notice">
          <TriangleAlert size={18} />
          <span>{detailError}</span>
        </div>
      )}
      {detailLoading && <div className="loading-panel">{t.loadingService}</div>}
      {selectedService && detail && <ServiceDetailPanel detail={detail} lang={lang} />}
    </section>
  );
}

function ServiceDetailPanel({ detail, lang }: { detail: ServiceDetailResponse; lang: Lang }) {
  const t = dictionary[lang];
  const latestCheck = detail.latest_check;
  const [checks, setChecks] = useState<ServiceCheckHistoryResponse | null>(null);
  const [checksError, setChecksError] = useState('');
  const [checksLoading, setChecksLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const loadChecks = async () => {
      setChecksLoading(true);
      try {
        const next = await fetchServiceChecks(detail.service.id, '24h', 12);
        if (!cancelled) {
          setChecks(next);
          setChecksError('');
        }
      } catch (err) {
        if (!cancelled) {
          setChecksError(err instanceof Error ? err.message : t.checksError);
        }
      } finally {
        if (!cancelled) {
          setChecksLoading(false);
        }
      }
    };
    void loadChecks();
    return () => {
      cancelled = true;
    };
  }, [detail.service.id, t.checksError]);

  return (
    <section className="panel detail-panel">
      <div className="detail-head">
        <div>
          <div className="eyebrow">{t.serviceDetails}</div>
          <h2>{detail.service.name}</h2>
        </div>
        <StatusPill status={detail.service.status} lang={lang} />
      </div>
      {detail.service.description && <p className="project-detail-summary">{detail.service.description}</p>}

      <div className="detail-stat-grid">
        <Metric icon={Server} label={t.node} value={detail.node?.name ?? detail.service.node_id} />
        <Metric icon={Layers3} label={t.project} value={detail.project?.name ?? detail.service.project_id} />
        <Metric icon={Clock} label={t.last} value={formatTime(detail.service.last_checked_at)} />
        <Metric icon={Activity} label={t.latency} value={detail.service.response_time_ms > 0 ? `${detail.service.response_time_ms}ms` : '-'} />
      </div>

      <div className="project-detail-grid">
        <SubPanel title={t.latestCheck}>
          <div className="diag-kv compact-kv">
            <span>{t.endpoint}</span>
            <strong>{detail.service.endpoint_key || '-'}</strong>
            <span>{t.target}</span>
            <strong>{detail.service.target || '-'}</strong>
            <span>{t.kind}</span>
            <strong>{detail.service.kind || '-'}</strong>
            <span>{t.detail}</span>
            <strong>{latestCheck?.detail || detail.service.detail || '-'}</strong>
          </div>
        </SubPanel>

        <SubPanel title={t.relatedMetrics}>
          {!detail.metrics ? (
            <div className="empty inline-empty">{t.noMetrics}</div>
          ) : (
            <MetricSummaryCard metric={detail.metrics} lang={lang} />
          )}
        </SubPanel>
      </div>

      <SubPanel title={t.checkHistory}>
        {checksError && (
          <div className="notice inline-notice">
            <TriangleAlert size={18} />
            <span>{checksError}</span>
          </div>
        )}
        {checksLoading && <div className="loading-panel inline-empty">{t.loadingChecks}</div>}
        {!checksLoading && checks && checks.results.length === 0 && <div className="empty inline-empty">{t.noCheckHistory}</div>}
        {checks && checks.results.length > 0 && (
          <div className="check-history-list">
            {checks.results.map((result) => (
              <article className="check-history-item" key={`${result.timestamp}-${result.response_time_ms}-${result.detail}`}>
                <StatusDot status={result.status} />
                <div>
                  <div className="check-history-head">
                    <strong>{statusLabel[lang][result.status]}</strong>
                    <span>{formatTime(result.timestamp)} · {result.response_time_ms > 0 ? `${result.response_time_ms}ms` : '-'}</span>
                  </div>
                  <p>{result.detail}</p>
                  {result.errors && result.errors.length > 0 && <span>{t.errors}: {result.errors.join('; ')}</span>}
                  {result.conditions && result.conditions.length > 0 && (
                    <span>
                      {t.conditions}: {result.conditions.map((condition) => `${condition.success ? 'OK' : 'FAIL'} ${condition.condition}`).join('; ')}
                    </span>
                  )}
                </div>
              </article>
            ))}
          </div>
        )}
      </SubPanel>

      <SubPanel title={t.serviceEvents}>
        {detail.events.length === 0 ? (
          <div className="empty inline-empty">{t.noServiceEvents}</div>
        ) : (
          <div className="mini-list">
            {detail.events.map((event) => (
              <Row key={event.id} title={event.label} subtitle={`${event.kind} · ${formatTime(event.created_at)}`} status={event.to} meta={event.detail || statusLabel[lang][event.to]} />
            ))}
          </div>
        )}
      </SubPanel>
    </section>
  );
}
