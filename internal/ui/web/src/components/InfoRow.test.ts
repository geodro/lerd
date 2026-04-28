import { render, screen } from '@testing-library/svelte';
import { describe, it, expect } from 'vitest';
import InfoRow from './InfoRow.svelte';

describe('InfoRow', () => {
  it('renders label and mono value', () => {
    render(InfoRow, { props: { label: 'TLD', value: '.test' } });
    expect(screen.getByText('TLD')).toBeInTheDocument();
    const val = screen.getByText('.test');
    expect(val.tagName.toLowerCase()).toBe('code');
  });

  it('uses plain span when mono=false', () => {
    render(InfoRow, { props: { label: 'Running', value: 'yes', mono: false } });
    expect(screen.getByText('yes').tagName.toLowerCase()).toBe('span');
  });
});
