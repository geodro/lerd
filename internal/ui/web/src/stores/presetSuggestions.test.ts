import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { presets, type Preset } from './presets';
import type { Service } from './services';
import {
  detectServiceFamily,
  suggestedPresetFor,
  suggestionFor,
  dismissedSuggestions
} from './presetSuggestions';

function svc(over: Partial<Service> & { name: string }): Service {
  return { status: 'active', site_count: 0, ...over };
}

const pgadmin: Preset = { name: 'pgadmin', description: 'Postgres admin UI', installed: false };

beforeEach(() => {
  presets.set([pgadmin]);
  dismissedSuggestions.set([]);
});

describe('detectServiceFamily', () => {
  it('maps a bare family name to itself', () => {
    expect(detectServiceFamily(svc({ name: 'postgres' }))).toBe('postgres');
    expect(detectServiceFamily(svc({ name: 'mysql' }))).toBe('mysql');
  });

  it('maps a versioned service name to a known family via the prefix', () => {
    expect(detectServiceFamily(svc({ name: 'postgres-17' }))).toBe('postgres');
    expect(detectServiceFamily(svc({ name: 'mysql-8' }))).toBe('mysql');
  });

  it('maps via the connection_url scheme when the name prefix is not a family key', () => {
    expect(detectServiceFamily(svc({ name: 'pg', connection_url: 'postgresql://x' }))).toBe(
      'postgres'
    );
    expect(
      detectServiceFamily(svc({ name: 'mariadb-11', connection_url: 'mysql://x' }))
    ).toBe('mysql');
  });

  it('returns null for an unknown service and for nullish input', () => {
    expect(detectServiceFamily(svc({ name: 'redis' }))).toBeNull();
    expect(detectServiceFamily(null)).toBeNull();
    expect(detectServiceFamily(undefined)).toBeNull();
  });
});

describe('suggestedPresetFor', () => {
  it('suggests pgadmin for a versioned postgres-17 service', () => {
    expect(suggestedPresetFor(svc({ name: 'postgres-17' }))?.name).toBe('pgadmin');
  });

  it('still suggests pgadmin for the bare postgres service', () => {
    expect(suggestedPresetFor(svc({ name: 'postgres' }))?.name).toBe('pgadmin');
  });

  it('returns null once the suggestion is dismissed', () => {
    dismissedSuggestions.set(['pgadmin']);
    expect(suggestedPresetFor(svc({ name: 'postgres-17' }))).toBeNull();
  });

  it('returns null when the admin tool is already installed', () => {
    presets.set([{ ...pgadmin, installed: true }]);
    expect(suggestedPresetFor(svc({ name: 'postgres-17' }))).toBeNull();
  });

  it('returns null when the admin tool has an unmet dependency', () => {
    presets.set([{ ...pgadmin, missing_deps: ['postgres'] }]);
    expect(suggestedPresetFor(svc({ name: 'postgres-17' }))).toBeNull();
  });
});

describe('suggestionFor', () => {
  it('reactively suggests pgadmin for postgres-17', () => {
    expect(get(suggestionFor(svc({ name: 'postgres-17' })))?.name).toBe('pgadmin');
  });

  it('drops the suggestion when dismissed', () => {
    dismissedSuggestions.set(['pgadmin']);
    expect(get(suggestionFor(svc({ name: 'postgres-17' })))).toBeNull();
  });

  it('drops the suggestion when the admin tool has an unmet dependency', () => {
    presets.set([{ ...pgadmin, missing_deps: ['postgres'] }]);
    expect(get(suggestionFor(svc({ name: 'postgres-17' })))).toBeNull();
  });
});
