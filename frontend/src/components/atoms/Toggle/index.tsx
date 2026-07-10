import type { ComponentPropsWithoutRef } from 'react';
import { Knob, Switch } from './styled';

type Props = { on: boolean } & ComponentPropsWithoutRef<'button'>;

export function Toggle({ on, ...rest }: Props) {
  return (
    <Switch $on={on} role="switch" aria-checked={on} {...rest}>
      <Knob />
    </Switch>
  );
}

export default Toggle;
