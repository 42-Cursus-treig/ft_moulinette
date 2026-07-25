#include "ft_list.h"
#include <unistd.h>

void	ft_sorted_list_insert(t_list **begin_list, void *data, int (*cmp)());

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static void	print_list_str(t_list *lst)
{
	while (lst)
	{
		put_str((char *)lst->data);
		write(1, "\n", 1);
		lst = lst->next;
	}
}

static int	ft_strcmp(char *a, char *b)
{
	int	i;

	i = 0;
	while (a[i] && a[i] == b[i])
		i++;
	return ((unsigned char)a[i] - (unsigned char)b[i]);
}

int	main(int argc, char **argv)
{
	t_list	*begin;
	int		i;

	begin = NULL;
	i = 1;
	while (i < argc)
	{
		ft_sorted_list_insert(&begin, argv[i], &ft_strcmp);
		i++;
	}
	print_list_str(begin);
	return (0);
}
