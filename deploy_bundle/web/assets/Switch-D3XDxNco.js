import{$t as e,An as t,Ar as n,At as r,Br as i,C as a,E as o,En as s,Et as c,It as l,Nt as u,On as d,Rr as f,Sn as p,Tn as m,Tt as h,Yt as g,_,fi as v,gi as y,jn as b,k as x,kn as S,wt as C,y as w,yn as T}from"./Card-CkLGnFJ7.js";var E={buttonHeightSmall:`14px`,buttonHeightMedium:`18px`,buttonHeightLarge:`22px`,buttonWidthSmall:`14px`,buttonWidthMedium:`18px`,buttonWidthLarge:`22px`,buttonWidthPressedSmall:`20px`,buttonWidthPressedMedium:`24px`,buttonWidthPressedLarge:`28px`,railHeightSmall:`18px`,railHeightMedium:`22px`,railHeightLarge:`26px`,railWidthSmall:`32px`,railWidthMedium:`40px`,railWidthLarge:`48px`};function D(t){let{primaryColor:n,opacityDisabled:r,borderRadius:i,textColor3:a}=t;return Object.assign(Object.assign({},E),{iconColor:a,textColor:`white`,loadingColor:n,opacityDisabled:r,railColor:`rgba(0, 0, 0, .14)`,railColorActive:n,buttonBoxShadow:`0 1px 4px 0 rgba(0, 0, 0, 0.3), inset 0 0 1px 0 rgba(0, 0, 0, 0.05)`,buttonColor:`#FFF`,railBorderRadiusSmall:i,railBorderRadiusMedium:i,railBorderRadiusLarge:i,buttonBorderRadiusSmall:i,buttonBorderRadiusMedium:i,buttonBorderRadiusLarge:i,boxShadowFocus:`0 0 0 2px ${e(n,{alpha:.2})}`})}var O={name:`Switch`,common:_,self:D},k=s(`switch`,`
 height: var(--n-height);
 min-width: var(--n-width);
 vertical-align: middle;
 user-select: none;
 -webkit-user-select: none;
 display: inline-flex;
 outline: none;
 justify-content: center;
 align-items: center;
`,[d(`children-placeholder`,`
 height: var(--n-rail-height);
 display: flex;
 flex-direction: column;
 overflow: hidden;
 pointer-events: none;
 visibility: hidden;
 `),d(`rail-placeholder`,`
 display: flex;
 flex-wrap: none;
 `),d(`button-placeholder`,`
 width: calc(1.75 * var(--n-rail-height));
 height: var(--n-rail-height);
 `),s(`base-loading`,`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 font-size: calc(var(--n-button-width) - 4px);
 color: var(--n-loading-color);
 transition: color .3s var(--n-bezier);
 `,[a({left:`50%`,top:`50%`,originalTransform:`translateX(-50%) translateY(-50%)`})]),d(`checked, unchecked`,`
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 box-sizing: border-box;
 position: absolute;
 white-space: nowrap;
 top: 0;
 bottom: 0;
 display: flex;
 align-items: center;
 line-height: 1;
 `),d(`checked`,`
 right: 0;
 padding-right: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),d(`unchecked`,`
 left: 0;
 justify-content: flex-end;
 padding-left: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),m(`&:focus`,[d(`rail`,`
 box-shadow: var(--n-box-shadow-focus);
 `)]),S(`round`,[d(`rail`,`border-radius: calc(var(--n-rail-height) / 2);`,[d(`button`,`border-radius: calc(var(--n-button-height) / 2);`)])]),t(`disabled`,[t(`icon`,[S(`rubber-band`,[S(`pressed`,[d(`rail`,[d(`button`,`max-width: var(--n-button-width-pressed);`)])]),d(`rail`,[m(`&:active`,[d(`button`,`max-width: var(--n-button-width-pressed);`)])]),S(`active`,[S(`pressed`,[d(`rail`,[d(`button`,`left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));`)])]),d(`rail`,[m(`&:active`,[d(`button`,`left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));`)])])])])])]),S(`active`,[d(`rail`,[d(`button`,`left: calc(100% - var(--n-button-width) - var(--n-offset))`)])]),d(`rail`,`
 overflow: hidden;
 height: var(--n-rail-height);
 min-width: var(--n-rail-width);
 border-radius: var(--n-rail-border-radius);
 cursor: pointer;
 position: relative;
 transition:
 opacity .3s var(--n-bezier),
 background .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 background-color: var(--n-rail-color);
 `,[d(`button-icon`,`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 font-size: calc(var(--n-button-height) - 4px);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 display: flex;
 justify-content: center;
 align-items: center;
 line-height: 1;
 `,[a()]),d(`button`,`
 align-items: center; 
 top: var(--n-offset);
 left: var(--n-offset);
 height: var(--n-button-height);
 width: var(--n-button-width-pressed);
 max-width: var(--n-button-width);
 border-radius: var(--n-button-border-radius);
 background-color: var(--n-button-color);
 box-shadow: var(--n-button-box-shadow);
 box-sizing: border-box;
 cursor: inherit;
 content: "";
 position: absolute;
 transition:
 background-color .3s var(--n-bezier),
 left .3s var(--n-bezier),
 opacity .3s var(--n-bezier),
 max-width .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 `)]),S(`active`,[d(`rail`,`background-color: var(--n-rail-color-active);`)]),S(`loading`,[d(`rail`,`
 cursor: wait;
 `)]),S(`disabled`,[d(`rail`,`
 cursor: not-allowed;
 opacity: .5;
 `)])]),A=Object.assign(Object.assign({},x.props),{size:String,value:{type:[String,Number,Boolean],default:void 0},loading:Boolean,defaultValue:{type:[String,Number,Boolean],default:!1},disabled:{type:Boolean,default:void 0},round:{type:Boolean,default:!0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],checkedValue:{type:[String,Number,Boolean],default:!0},uncheckedValue:{type:[String,Number,Boolean],default:!1},railStyle:Function,rubberBand:{type:Boolean,default:!0},spinProps:Object,onChange:[Function,Array]}),j,M=f({name:`Switch`,props:A,slots:Object,setup(e){j===void 0&&(j=typeof CSS<`u`?CSS.supports!==void 0&&CSS.supports(`width`,`max(1px)`):!0);let{mergedClsPrefixRef:t,inlineThemeDisabled:r,mergedComponentPropsRef:i}=c(e),a=x(`Switch`,`-switch`,k,O,e,t),o=C(e,{mergedSize(t){return e.size===void 0?t?t.mergedSize.value:i?.value?.Switch?.size||`medium`:e.size}}),{mergedSizeRef:s,mergedDisabledRef:u}=o,d=v(e.defaultValue),f=y(e,`value`),m=g(f,d),_=n(()=>m.value===e.checkedValue),S=v(!1),w=v(!1),E=n(()=>{let{railStyle:t}=e;if(t)return t({focused:w.value,checked:_.value})});function D(t){let{"onUpdate:value":n,onChange:r,onUpdateValue:i}=e,{nTriggerFormInput:a,nTriggerFormChange:s}=o;n&&l(n,t),i&&l(i,t),r&&l(r,t),d.value=t,a(),s()}function A(){let{nTriggerFormFocus:e}=o;e()}function M(){let{nTriggerFormBlur:e}=o;e()}function N(){e.loading||u.value||(m.value===e.checkedValue?D(e.uncheckedValue):D(e.checkedValue))}function P(){w.value=!0,A()}function F(){w.value=!1,M(),S.value=!1}function I(t){e.loading||u.value||t.key===` `&&(m.value===e.checkedValue?D(e.uncheckedValue):D(e.checkedValue),S.value=!1)}function L(t){e.loading||u.value||t.key===` `&&(t.preventDefault(),S.value=!0)}let R=n(()=>{let{value:e}=s,{self:{opacityDisabled:t,railColor:n,railColorActive:r,buttonBoxShadow:i,buttonColor:o,boxShadowFocus:c,loadingColor:l,textColor:u,iconColor:d,[b(`buttonHeight`,e)]:f,[b(`buttonWidth`,e)]:m,[b(`buttonWidthPressed`,e)]:h,[b(`railHeight`,e)]:g,[b(`railWidth`,e)]:_,[b(`railBorderRadius`,e)]:v,[b(`buttonBorderRadius`,e)]:y},common:{cubicBezierEaseInOut:x}}=a.value,S,C,w;return j?(S=`calc((${g} - ${f}) / 2)`,C=`max(${g}, ${f})`,w=`max(${_}, calc(${_} + ${f} - ${g}))`):(S=p((T(g)-T(f))/2),C=p(Math.max(T(g),T(f))),w=T(g)>T(f)?_:p(T(_)+T(f)-T(g))),{"--n-bezier":x,"--n-button-border-radius":y,"--n-button-box-shadow":i,"--n-button-color":o,"--n-button-width":m,"--n-button-width-pressed":h,"--n-button-height":f,"--n-height":C,"--n-offset":S,"--n-opacity-disabled":t,"--n-rail-border-radius":v,"--n-rail-color":n,"--n-rail-color-active":r,"--n-rail-height":g,"--n-rail-width":_,"--n-width":w,"--n-box-shadow-focus":c,"--n-loading-color":l,"--n-text-color":u,"--n-icon-color":d}}),z=r?h(`switch`,n(()=>s.value[0]),R,e):void 0;return{handleClick:N,handleBlur:F,handleFocus:P,handleKeyup:I,handleKeydown:L,mergedRailStyle:E,pressed:S,mergedClsPrefix:t,mergedValue:m,checked:_,mergedDisabled:u,cssVars:r?void 0:R,themeClass:z?.themeClass,onRender:z?.onRender}},render(){let{mergedClsPrefix:e,mergedDisabled:t,checked:n,mergedRailStyle:a,onRender:s,$slots:c}=this;s?.();let{checked:l,unchecked:d,icon:f,"checked-icon":p,"unchecked-icon":m}=c,h=!(r(f)&&r(p)&&r(m));return i(`div`,{role:`switch`,"aria-checked":n,class:[`${e}-switch`,this.themeClass,h&&`${e}-switch--icon`,n&&`${e}-switch--active`,t&&`${e}-switch--disabled`,this.round&&`${e}-switch--round`,this.loading&&`${e}-switch--loading`,this.pressed&&`${e}-switch--pressed`,this.rubberBand&&`${e}-switch--rubber-band`],tabindex:this.mergedDisabled?void 0:0,style:this.cssVars,onClick:this.handleClick,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeyup:this.handleKeyup,onKeydown:this.handleKeydown},i(`div`,{class:`${e}-switch__rail`,"aria-hidden":`true`,style:a},u(l,t=>u(d,n=>t||n?i(`div`,{"aria-hidden":!0,class:`${e}-switch__children-placeholder`},i(`div`,{class:`${e}-switch__rail-placeholder`},i(`div`,{class:`${e}-switch__button-placeholder`}),t),i(`div`,{class:`${e}-switch__rail-placeholder`},i(`div`,{class:`${e}-switch__button-placeholder`}),n)):null)),i(`div`,{class:`${e}-switch__button`},u(f,t=>u(p,n=>u(m,r=>i(o,null,{default:()=>this.loading?i(w,Object.assign({key:`loading`,clsPrefix:e,strokeWidth:20},this.spinProps)):this.checked&&(n||t)?i(`div`,{class:`${e}-switch__button-icon`,key:n?`checked-icon`:`icon`},n||t):!this.checked&&(r||t)?i(`div`,{class:`${e}-switch__button-icon`,key:r?`unchecked-icon`:`icon`},r||t):null})))),u(l,t=>t&&i(`div`,{key:`checked`,class:`${e}-switch__checked`},t)),u(d,t=>t&&i(`div`,{key:`unchecked`,class:`${e}-switch__unchecked`},t)))))}});export{A as n,M as t};