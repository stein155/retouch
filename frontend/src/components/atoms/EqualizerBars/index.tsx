import type * as React from 'react';
import { Bars } from './styled';

export function EqualizerBars(props: React.ComponentProps<typeof Bars>) {
  return (
    <Bars {...props}>
      <i /><i /><i /><i />
    </Bars>
  );
}

export default EqualizerBars;
