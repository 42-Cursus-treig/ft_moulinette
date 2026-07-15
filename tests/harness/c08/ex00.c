#include "ft.h"

void	(*g_putchar)(char) = &ft_putchar;
void	(*g_swap)(int *, int *) = &ft_swap;
void	(*g_putstr)(char *) = &ft_putstr;
int		(*g_strlen)(char *) = &ft_strlen;
int		(*g_strcmp)(char *, char *) = &ft_strcmp;