import { DataSourceInstanceSettings, CoreApp, DataQueryRequest, AnnotationQueryRequest, AnnotationEvent, DataFrame, toDataFrame } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';
import { FirebirdDataSourceOptions, FirebirdQuery } from './types';
import { QueryEditor } from './components/QueryEditor';
import { ConfigEditor } from './components/ConfigEditor';

export class DataSource extends DataSourceWithBackend<FirebirdQuery, FirebirdDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<FirebirdDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<FirebirdQuery> {
    return {
      refId: 'A',
      rawSql: 'SELECT 1 AS value FROM RDB$DATABASE',
      format: 'table',
    };
  }

  filterQuery(query: FirebirdQuery): boolean {
    return !!query.rawSql;
  }

  /**
   * Template variable support: run a variable query and return the options.
   * The user writes a SELECT returning one or two columns:
   *   - one column: value and label are the same
   *   - two columns: first = value (__text), second = label (__text shown)
   *     (Grafana convention: first column is "text", second is "value" for
   *     key/value variables; we map first -> value, second -> text)
   */
  async metricFindQuery(rawSql: string, options?: any) {
    const query: FirebirdQuery = {
      refId: 'metricFindQuery',
      rawSql,
      format: 'table',
    };
    const frame = await this.queryDatabaseSingleFrame(query);
    if (!frame) {
      return [];
    }
    const out: Array<{ text: any; value: any }> = [];
    const cols = frame.fields;
    if (cols.length === 1) {
      for (let i = 0; i < frame.length; i++) {
        const v = cols[0].values.get(i);
        out.push({ text: String(v), value: String(v) });
      }
    } else {
      // two+ columns: first = value, second = display text
      for (let i = 0; i < frame.length; i++) {
        const v = cols[0].values.get(i);
        const t = cols[1].values.get(i);
        out.push({ text: String(t), value: String(v) });
      }
    }
    return out;
  }

  /**
   * Annotation support: the user configures an annotation query returning
   * columns `time` (mandatory) and optional `text` and `tags`.
   * Events are fetched by re-running the raw SQL with the annotation request's
   * time range via the backend (macros get expanded server-side).
   */
  async annotationQuery(request: AnnotationQueryRequest<FirebirdQuery>): Promise<AnnotationEvent[]> {
    if (!request.annotation.rawSql) {
      return [];
    }
    const query: FirebirdQuery = {
      refId: 'annotationQuery',
      rawSql: request.annotation.rawSql,
      format: 'table',
    };
    const frame = await this.queryDatabaseSingleFrame(query, request.range);
    if (!frame) {
      return [];
    }
    const timeField = frame.fields.find((f) => f.name.toLowerCase() === 'time');
    if (!timeField) {
      return [];
    }
    const textField = frame.fields.find((f) => f.name.toLowerCase() === 'text');
    const tagsField = frame.fields.find((f) => f.name.toLowerCase() === 'tags');

    const events: AnnotationEvent[] = [];
    for (let i = 0; i < frame.length; i++) {
      const raw = timeField.values.get(i);
      const ts = raw instanceof Date ? raw.getTime() : new Date(raw).getTime();
      if (isNaN(ts)) {
        continue;
      }
      const tags = tagsField
        ? String(tagsField.values.get(i))
            .split(',')
            .map((t) => t.trim())
            .filter(Boolean)
        : [];
      events.push({
        time: ts,
        text: textField ? String(textField.values.get(i)) : undefined,
        tags: tags.length ? tags : undefined,
      });
    }
    return events;
  }

  /**
   * Run a single query through the backend and return the first frame.
   * Uses the standard query path so server-side macros, maxRows and the
   * read-only validation all apply.
   */
  private async queryDatabaseSingleFrame(query: FirebirdQuery, range?: any): Promise<DataFrame | undefined> {
    const rsp = await this.query({
      targets: [query],
      range: range ?? { from: new Date(Date.now() - 3600_000), to: new Date() },
      requestId: `firebird-var-${Math.random().toString(36).slice(2)}`,
    } as unknown as DataQueryRequest<FirebirdQuery>).toPromise();
    const frames = rsp?.data ?? [];
    return frames.length ? frames[0] : undefined;
  }
}

export { QueryEditor, ConfigEditor };
