import { StylePreset } from './types';

// Display-only until M2: Go serves no presets endpoint yet, so these are not authoritative.
export const PREVIEW_PRESETS: StylePreset[] = [
  { id: 'old_money', name: 'Old Money', category: 'fashion', description: 'Classic, understated luxury with tailored cashmere and timeless heritage elegance.', tags: ['Tailored', 'Cashmere', 'Heritage'] },
  { id: 'minimalist', name: 'Minimalist', category: 'fashion', description: 'Clean lines, monochrome neutrals and architectural silhouettes.', tags: ['Clean Lines', 'Monochrome'] },
  { id: 'streetwear', name: 'Streetwear', category: 'fashion', description: 'Relaxed oversized cuts, technical textures and designer sneakers.', tags: ['Oversized', 'Urban'] },
  { id: 'smart_casual', name: 'Smart Casual', category: 'fashion', description: 'Structured blazers with chic knitwear for day-to-evening.', tags: ['Blazer', 'Knitwear'] },
  { id: 'formal_elegant', name: 'Formal Elegant', category: 'fashion', description: 'Black tie sophistication with opulent fabrics and immaculate tailoring.', tags: ['Black Tie', 'Couture'] },
];
