import { Knob, Switch } from './styled';

export function Toggle({ on, ...rest }) {
  return (
    <Switch $on={on} role="switch" aria-checked={on} {...rest}>
      <Knob />
    </Switch>
  );
}

export default Toggle;
