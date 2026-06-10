export type MetricRange = '1h' | '24h' | '7d';

export const metricRanges: MetricRange[] = ['1h', '24h', '7d'];

export function initialMetricRange(): MetricRange {
  const range = new URLSearchParams(window.location.search).get('range');
  return metricRanges.includes(range as MetricRange) ? (range as MetricRange) : '1h';
}
